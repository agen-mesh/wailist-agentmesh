import type { PaymentMethod } from "@/components/checkout/types";

// Credit wallet model. With a backend configured, the balance comes from
// GET /credits/balance and history from GET /credits/history, so both are
// per-account rather than per-browser, and only the auto-recharge preference
// stays in localStorage. Without one, history and preferences fall back to a
// per-browser localStorage mock. Amounts paid are INR; credits are denominated
// in USD (via a mock FX in mock mode).

// "paid" is a completed, credited purchase. Real DB-backed history can also be
// pending (awaiting confirmation), failed/expired, or refunded.
export type PurchaseStatus = "paid" | "pending" | "failed" | "refunded";

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
