import type { PaymentMethod } from "./types";

// Single source of truth for the checkout's payment providers and their live
// state. Razorpay + NOWPayments are selectable today; PayPal + Stripe render as
// disabled "coming soon" rows. Order here is the display order.
export interface PaymentProvider {
  id: PaymentMethod;
  label: string;
  sublabel: string;
  enabled: boolean;
}

export const PAYMENT_PROVIDERS: PaymentProvider[] = [
  {
    id: "razorpay",
    label: "Razorpay",
    sublabel: "UPI, cards & netbanking · INR",
    enabled: true,
  },
  {
    id: "nowpayments",
    label: "NOWPayments",
    sublabel: "Pay with crypto",
    enabled: true,
  },
  {
    id: "paypal",
    label: "PayPal",
    sublabel: "Coming soon",
    enabled: false,
  },
  {
    id: "stripe",
    label: "Stripe",
    sublabel: "Coming soon",
    enabled: false,
  },
];

// The default selected provider — the first enabled one.
export const DEFAULT_PROVIDER: PaymentMethod =
  PAYMENT_PROVIDERS.find((p) => p.enabled)?.id ?? "razorpay";
