import type { PurchaseAuthorization, PurchaseConfig, PurchaseOffer, PurchaseOrder } from "@/lib/purchase";

export const TEST_NOW = Date.parse("2026-11-05T12:00:00Z");
export const PAYER = `0x${"1".repeat(40)}`;
export const PAYEE = `0x${"2".repeat(40)}`;
export const config: PurchaseConfig = {
  enabled: true, blockedReasons: [], chainId: 5042002,
  asset: "0x3600000000000000000000000000000000000000",
  rpcUrl: "https://rpc.testnet.arc.io", merchantCapability: "text-summary",
  arkivAvailable: true, worldAvailable: true, evidenceMode: "simulation",
};

export function offer(overrides: Partial<PurchaseOffer> = {}): PurchaseOffer {
  return {
    key: `0x${"b".repeat(64)}`, owner: PAYEE, version: "1", contentHash: `0x${"c".repeat(64)}`,
    snapshotHash: `0x${"d".repeat(64)}`, endpoint: "https://merchant.example/purchase", method: "POST",
    capability: "text-summary", inputSchema: { type: "object", properties: {}, required: [], additionalProperties: false },
    chainId: config.chainId, asset: config.asset, payee: PAYEE, priceMicros: "10000",
    validUntil: "2026-11-05T13:00:00Z", expiresAtBlock: "500", observedBlock: "499", ...overrides,
  };
}

export function order(overrides: Partial<PurchaseOrder> = {}): PurchaseOrder {
  return {
    id: "order-1", intentHash: `0x${"a".repeat(64)}`, state: "approved", offer: offer(),
    payer: PAYER, payee: PAYEE, chainId: config.chainId, asset: config.asset,
    amountMicros: "10000", maxMicros: "10000", method: "POST", resource: "https://merchant.example/purchase",
    requestDigest: `0x${"e".repeat(64)}`, quoteDigest: `0x${"f".repeat(64)}`,
    deadline: "2026-11-05T12:10:00Z", identityVerifiedAt: "2026-11-05T11:59:55Z", approvedAt: "2026-11-05T11:59:58Z",
    canApprove: false, canAuthorize: true, canCancel: true, canReconcile: false, ...overrides,
  };
}

export function authorization(): PurchaseAuthorization {
  const original = order();
  return {
    order: { ...original, state: "authorization_released", signatureReleasedAt: "2026-11-05T12:00:00Z", canAuthorize: false, canCancel: false, canReconcile: true },
    typedData: {
      domain: { name: "USDC", version: "2", chainId: config.chainId, verifyingContract: config.asset },
      primaryType: "TransferWithAuthorization",
      types: {
        EIP712Domain: [{ name: "name", type: "string" }, { name: "version", type: "string" }, { name: "chainId", type: "uint256" }, { name: "verifyingContract", type: "address" }],
        TransferWithAuthorization: [
          { name: "from", type: "address" }, { name: "to", type: "address" }, { name: "value", type: "uint256" },
          { name: "validAfter", type: "uint256" }, { name: "validBefore", type: "uint256" }, { name: "nonce", type: "bytes32" },
        ],
      },
      message: { from: PAYER, to: PAYEE, value: "10000", validAfter: "0", validBefore: String(Date.parse(original.deadline) / 1000), nonce: original.intentHash },
    },
  };
}
