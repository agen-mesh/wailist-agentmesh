import { apiFetch, BASE } from "./api";

export type PurchaseState =
  | "awaiting_identity" | "awaiting_approval" | "approved"
  | "authorization_released" | "submitting" | "payment_unknown"
  | "paid" | "fulfilling" | "completed" | "paid_tool_failed"
  | "fulfillment_unknown" | "denied" | "expired" | "cancelled";

export interface PurchaseConfig {
  enabled: boolean;
  blockedReasons: string[];
  chainId: number;
  asset: string;
  rpcUrl: string;
  merchantCapability: string;
  arkivAvailable: boolean;
  arkivChainId: number;
  worldAvailable: boolean;
  evidenceMode: "live" | "simulation";
}

export interface PurchaseOffer {
  key: string;
  owner: string;
  version: string;
  contentHash: string;
  snapshotHash: string;
  endpoint: string;
  method: "POST";
  capability: string;
  inputSchema: Record<string, unknown>;
  chainId: number;
  asset: string;
  payee: string;
  priceMicros: string;
  validUntil: string;
  expiresAtBlock: string;
  observedBlock: string;
}

export interface PurchaseWallet {
  address: string;
  verifiedAt: string;
}

export interface PurchaseAllowance {
  wallet: string;
  chainId: number;
  asset: string;
  limitMicros: string;
  reservedMicros: string;
  consumedMicros: string;
  availableMicros: string;
}

export interface PurchasePaymentEvent {
  chainId: number;
  txHash: string;
  logIndex: string;
  blockHash: string;
  blockNumber: string;
}

export interface PurchaseOrder {
  id: string;
  intentHash: string;
  state: PurchaseState;
  offer: PurchaseOffer;
  payer: string;
  payee: string;
  chainId: number;
  asset: string;
  amountMicros: string;
  maxMicros: string;
  method: "POST";
  resource: string;
  requestDigest: string;
  quoteDigest: string;
  deadline: string;
  identityVerifiedAt?: string;
  approvedAt?: string;
  signatureReleasedAt?: string;
  txHash?: string;
  paymentEvent?: PurchasePaymentEvent;
  result?: unknown;
  reason?: string;
  canApprove: boolean;
  canAuthorize: boolean;
  canCancel: boolean;
  canReconcile: boolean;
}

export interface PurchaseAuthorization {
  order: PurchaseOrder;
  typedData: {
    domain: { name: string; version: string; chainId: number; verifyingContract: string };
    types: Record<string, { name: string; type: string }[]>;
    primaryType: "TransferWithAuthorization";
    message: { from: string; to: string; value: string; validAfter: string; validBefore: string; nonce: string };
  };
}

export class PurchaseAPIError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
    this.name = "PurchaseAPIError";
  }
}

async function request<T>(path: string, body?: unknown): Promise<T> {
  if (!BASE) throw new PurchaseAPIError("Purchase backend is not configured", 503);
  const response = await apiFetch(`${BASE}/purchases${path}`, {
    method: body === undefined ? "GET" : "POST",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    cache: "no-store",
  });
  const data = await response.json().catch(() => null);
  if (!response.ok) throw new PurchaseAPIError(data?.error ?? `Purchase request failed (${response.status})`, response.status);
  return data as T;
}

const orderPath = (id: string) => `/${encodeURIComponent(id)}`;

export const purchaseAPI = {
  config: () => request<PurchaseConfig>("/config"),
  offers: (filter: { capability?: string; maxPriceMicros?: string } = {}) => {
    const params = new URLSearchParams();
    if (filter.capability) params.set("capability", filter.capability);
    if (filter.maxPriceMicros) params.set("maxPriceMicros", filter.maxPriceMicros);
    return request<{ offers: PurchaseOffer[] }>(`/offers?${params}`);
  },
  preflightOfferEndpoint: (endpoint: string) => request<{ valid: true }>("/offers/preflight", { endpoint }),
  wallet: () => request<PurchaseWallet | null>("/wallet"),
  challengeWallet: (address: string) => request<{ id: string; message: string; expiresAt: string }>("/wallet/challenge", { address }),
  linkWallet: (challengeId: string, signature: string) => request<PurchaseWallet>("/wallet/link", { challengeId, signature }),
  allowance: () => request<PurchaseAllowance>("/allowance"),
  setAllowance: (limitMicros: string) => request<PurchaseAllowance>("/allowance", { limitMicros }),
  createOrder: (body: { offerKey: string; request: unknown; maxMicros: string; clientRequestId: string }) => request<PurchaseOrder>("", body),
  getOrder: (id: string) => request<PurchaseOrder>(orderPath(id)),
  beginIdentity: (id: string) => request<{ authorizationUrl: string; expiresAt: string }>(`${orderPath(id)}/identity`, {}),
  decideOrder: (id: string, intentHash: string, decision: "approve" | "deny") => request<PurchaseOrder>(`${orderPath(id)}/decision`, { intentHash, decision }),
  prepareAuthorization: (id: string) => request<PurchaseAuthorization>(`${orderPath(id)}/authorization`, {}),
  submitAuthorization: (id: string, signature: string) => request<PurchaseOrder>(`${orderPath(id)}/submit`, { signature }),
  reconcileOrder: (id: string) => request<PurchaseOrder>(`${orderPath(id)}/reconcile`, {}),
  cancelOrder: (id: string) => request<PurchaseOrder>(`${orderPath(id)}/cancel`, {}),
};
