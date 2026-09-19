"use client";
import { useRouter } from "next/navigation";
import { IconPlus } from "@/components/ui";
import { useCredits } from "@/lib/credits/store";

const usd = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
});

// The credit balance on a phone: one 44px line with the figure beside its
// label and "+" at the far end, where the desktop page spends a whole card.
// The page reads the balance on mount and on pull-to-refresh; this only shows
// it.
export function CreditStrip() {
  const router = useRouter();
  const { balanceUSD, balanceKnown } = useCredits();
  return (
    <div className="wfp-strip">
      <span className="wfp-strip__label">Credit balance</span>
      <span className="wfp-strip__amount">
        {balanceKnown ? usd.format(balanceUSD) : "—"}
      </span>
      <button
        type="button"
        className="wfp-strip__add"
        aria-label="Add credits"
        onClick={() => router.push("/billing")}
      >
        <IconPlus size={14} />
      </button>
    </div>
  );
}
