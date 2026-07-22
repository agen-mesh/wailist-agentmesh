import type { PaymentMethod } from "@/components/checkout/types";

// Frontend-only credit wallet model. This is a mock persisted to localStorage —
// there is no backend, so balances and history are per-browser until a real API
// lands. Amounts paid are INR; credits are denominated in USD (via a mock FX).

export type PurchaseStatus = "paid";

export interface Purchase {
  id: string;
  createdAt: string; // ISO 8601
  amountINR: number; // amount charged
  creditsUSD: number; // credits granted (base + bonus)
  method: PaymentMethod;
  status: PurchaseStatus;
}

export interface AutoRecharge {
  enabled: boolean;
  thresholdUSD: number; // recharge when balance drops below this
  amountINR: number; // how much to top up each time
  monthlyCapINR: number | null; // optional spend ceiling
}

export interface CreditsState {
  balanceUSD: number;
  purchases: Purchase[]; // newest first
  autoRecharge: AutoRecharge;
}
