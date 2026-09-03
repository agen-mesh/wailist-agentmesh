import type { PaymentMethod } from "./types";

// Display metadata for the checkout's payment providers. Order here is the
// display order.
//
// `enabled` is only the fallback used before the server answers (and if it
// never does). Which gateways can actually take money is deployment state --
// Stripe and PayPal are configured per-environment, and the USD gateways need
// a live FX rate to be priced at all -- so the real answer comes from
// GET /payments/providers via usePaymentProviders.
export interface PaymentProvider {
  id: PaymentMethod;
  label: string;
  sublabel: string;
  enabled: boolean;
}

export const PAYMENT_PROVIDERS: PaymentProvider[] = [
  {
    id: "cashfree",
    label: "Cashfree",
    sublabel: "UPI, cards & netbanking · INR",
    enabled: true,
  },
  {
    id: "stripe",
    label: "Stripe",
    sublabel: "Cards & wallets · USD",
    enabled: true,
  },
  {
    id: "paypal",
    label: "PayPal",
    sublabel: "PayPal balance & cards · USD",
    enabled: true,
  },
  {
    id: "nowpayments",
    label: "NOWPayments",
    sublabel: "Crypto · USD",
    enabled: true,
  },
];

// Providers that settle in USD. The top-up UI is denominated in rupees, so
// these have to convert the chosen amount at the server's live rate before
// charging -- see usePaymentProviders' usdPerINR.
export const USD_PROVIDERS: ReadonlySet<PaymentMethod> = new Set<PaymentMethod>(
  ["stripe", "paypal", "nowpayments"],
);

// The default selected provider -- the first enabled one.
export const DEFAULT_PROVIDER: PaymentMethod =
  PAYMENT_PROVIDERS.find((p) => p.enabled)?.id ?? "cashfree";
