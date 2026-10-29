import type { PurchaseAuthorization, PurchaseOffer, PurchaseOrder } from "@/lib/purchase";

export const ARC_CHAIN = 5042002;
export const ARC_ASSET = "0x3600000000000000000000000000000000000000";
const UNSIGNED = /^(0|[1-9][0-9]*)$/;

export function usdc(micros: string): string {
  if (!UNSIGNED.test(micros)) return "Unavailable";
  const value = BigInt(micros);
  const fraction = (value % 1_000_000n).toString().padStart(6, "0").replace(/0+$/, "");
  return `${value / 1_000_000n}${fraction ? `.${fraction}` : ""}`;
}

export function toMicros(value: string): string {
  if (!/^(0|[1-9][0-9]*)(\.[0-9]{1,6})?$/.test(value)) {
    throw new Error("Enter a USDC amount with at most six decimal places.");
  }
  const [whole, fraction = ""] = value.split(".");
  return (BigInt(whole) * 1_000_000n + BigInt(fraction.padEnd(6, "0"))).toString();
}

export function expired(deadline: string, now = Date.now()): boolean {
  const at = Date.parse(deadline);
  return !Number.isFinite(at) || at <= now;
}

export function offerUnavailable(offer: PurchaseOffer, now = Date.now()): string | null {
  if (offer.chainId !== ARC_CHAIN || offer.asset.toLowerCase() !== ARC_ASSET) return "Unsupported settlement network or asset";
  if (!UNSIGNED.test(offer.priceMicros) || BigInt(offer.priceMicros) === 0n) return "Invalid price";
  if (!UNSIGNED.test(offer.expiresAtBlock) || !UNSIGNED.test(offer.observedBlock)) return "Expiry could not be checked";
  if (BigInt(offer.expiresAtBlock) <= BigInt(offer.observedBlock)) return "Expired on Arkiv";
  if (expired(offer.validUntil, now)) return "Offer deadline passed";
  return null;
}

export function safeOrderId(value: string | null): string | null {
  return value && value.length <= 256 && value.trim() === value && !/[\s/\\?#\u0000-\u001f]/.test(value) ? value : null;
}

export function storedOrderKey(accountId: string): string {
  return `agentmesh.purchase.${encodeURIComponent(accountId)}`;
}

export function receiptURL(order: PurchaseOrder): string | null {
  if (order.chainId !== ARC_CHAIN || !order.txHash || !/^0x[0-9a-fA-F]{64}$/.test(order.txHash)) return null;
  return `https://testnet.arcscan.app/tx/${order.txHash}`;
}

export function immutableOrder(order: PurchaseOrder): string {
  return JSON.stringify([order.id, order.intentHash, order.offer.key, order.offer.owner, order.offer.version,
    order.offer.contentHash, order.offer.snapshotHash, order.payer, order.payee, order.chainId, order.asset, order.amountMicros,
    order.maxMicros, order.method, order.resource, order.requestDigest, order.quoteDigest, order.deadline]);
}

function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Invalid payment authorization.");
  return value as Record<string, unknown>;
}

function integer(value: unknown): bigint {
  if (typeof value === "number" && Number.isSafeInteger(value) && value >= 0) return BigInt(value);
  if (typeof value === "string" && UNSIGNED.test(value)) return BigInt(value);
  throw new Error("Invalid payment authorization amount or time.");
}

export function assertAuthorizationMatches(reviewed: PurchaseOrder, prepared: PurchaseAuthorization, account: string, now = Date.now()): void {
  const order = prepared.order;
  if (immutableOrder(reviewed) !== immutableOrder(order)) throw new Error("Order terms changed. Review a new order before signing.");
  if (!reviewed.canAuthorize || !order.approvedAt || !order.identityVerifiedAt || !order.signatureReleasedAt || expired(order.deadline, now)) {
    throw new Error("This order is not ready for a payment signature. Refresh its status.");
  }
  if (order.chainId !== ARC_CHAIN || order.asset.toLowerCase() !== ARC_ASSET || account.toLowerCase() !== order.payer.toLowerCase() ||
    order.amountMicros !== order.offer.priceMicros || order.payee.toLowerCase() !== order.offer.payee.toLowerCase() ||
    order.payee.toLowerCase() !== order.offer.owner.toLowerCase()) {
    throw new Error("The connected wallet, network or token does not match this order.");
  }
  const typed = object(prepared.typedData);
  const domain = object(typed.domain);
  const message = object(typed.message);
  const types = object(typed.types);
  const expected = [
    { name: "from", type: "address" }, { name: "to", type: "address" }, { name: "value", type: "uint256" },
    { name: "validAfter", type: "uint256" }, { name: "validBefore", type: "uint256" }, { name: "nonce", type: "bytes32" },
  ];
  const fields = types.TransferWithAuthorization;
  const sameFields = Array.isArray(fields) && fields.length === expected.length && fields.every((field: unknown, index: number) => {
    const entry = object(field);
    return Object.keys(entry).length === 2 && entry.name === expected[index].name && entry.type === expected[index].type;
  });
  if (typed.primaryType !== "TransferWithAuthorization" || !sameFields ||
    integer(domain.chainId) !== BigInt(ARC_CHAIN) || typeof domain.verifyingContract !== "string" ||
    domain.verifyingContract.toLowerCase() !== ARC_ASSET || typeof domain.name !== "string" || !domain.name ||
    typeof domain.version !== "string" || !domain.version || typeof message.from !== "string" ||
    message.from.toLowerCase() !== order.payer.toLowerCase() || typeof message.to !== "string" ||
    message.to.toLowerCase() !== order.payee.toLowerCase() || integer(message.value) !== integer(order.amountMicros) ||
    integer(order.amountMicros) > integer(order.maxMicros) || integer(message.validBefore) !== BigInt(Math.floor(Date.parse(order.deadline) / 1000)) ||
    integer(message.validAfter) >= integer(message.validBefore) || integer(message.validAfter) > BigInt(Math.floor(now / 1000)) ||
    typeof message.nonce !== "string" || !/^0x[0-9a-fA-F]{64}$/.test(message.nonce) || message.nonce.toLowerCase() !== order.intentHash.toLowerCase()) {
    throw new Error("The wallet authorization does not match the exact approved purchase.");
  }
}
