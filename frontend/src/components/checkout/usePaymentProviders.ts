"use client";
import { useEffect, useState } from "react";
import { payments } from "@/lib/api";
import { PAYMENT_PROVIDERS, type PaymentProvider } from "./paymentProviders";

// What a provider's sublabel becomes when this deployment cannot take money
// through it. Derived rather than hardcoded in the static list, so a gateway
// whose credentials get added later starts describing itself properly without
// anyone remembering to edit a label.
const UNAVAILABLE_SUBLABEL = "Coming soon";

export interface ProviderState {
  providers: PaymentProvider[];
  /** Live INR->USD rate, used to price the USD gateways. 0 until loaded. */
  usdPerINR: number;
  loading: boolean;
}

// Resolves which providers this deployment can actually take money through.
//
// The static list in paymentProviders.ts carries only labels and ordering;
// availability is deployment state (Stripe/PayPal credentials, and whether an
// FX rate could be fetched at all), so it is asked for rather than assumed.
// On failure every provider is left disabled: offering a button that can only
// fail is worse than showing none, and the error surfaces at the panel.
export function usePaymentProviders(): ProviderState {
  const [state, setState] = useState<ProviderState>({
    providers: PAYMENT_PROVIDERS.map((p) => ({
      ...p,
      enabled: false,
      sublabel: UNAVAILABLE_SUBLABEL,
    })),
    usdPerINR: 0,
    loading: true,
  });

  useEffect(() => {
    let cancelled = false;
    payments
      .listProviders()
      .then((res) => {
        if (cancelled) return;
        const enabledByID = new Map(
          res.providers.map((p) => [p.id, p.enabled]),
        );
        setState({
          providers: PAYMENT_PROVIDERS.map((p) => {
            // An id the server didn't mention is one this build knows about
            // and that deployment does not -- treat it as off.
            const enabled = enabledByID.get(p.id) ?? false;
            return {
              ...p,
              enabled,
              sublabel: enabled ? p.sublabel : UNAVAILABLE_SUBLABEL,
            };
          }),
          usdPerINR: res.usd_per_inr ?? 0,
          loading: false,
        });
      })
      .catch(() => {
        // Availability is unknown, so nothing is offered -- and the rows have
        // to read as unavailable rather than keeping their live sublabels.
        if (!cancelled) setState((s) => ({ ...s, loading: false }));
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return state;
}
