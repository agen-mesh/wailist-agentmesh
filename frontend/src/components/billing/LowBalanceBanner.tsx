"use client";
import { useCredits } from "@/lib/credits/store";
import { useCurrency } from "@/lib/currency/store";

// Low-balance warning: shows once the server-reported balance falls below the
// account's own threshold. Gated on balanceKnown, not on a render count -- an
// unfetched balance reads as 0 and would flash this banner at every user on
// page load.
export function LowBalanceBanner({ onTopUp }: { onTopUp: () => void }) {
  const { balanceUSD, balanceKnown, lowBalanceThresholdUSD } = useCredits();
  const { formatBalance } = useCurrency();

  if (!balanceKnown || balanceUSD >= lowBalanceThresholdUSD) return null;

  return (
    <div
      role="status"
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "space-between",
        gap: 12,
        padding: "10px 14px",
        marginBottom: 16,
        borderRadius: "var(--r-2)",
        border: "1px solid rgba(255,181,71,0.35)",
        background: "var(--warm-soft)",
        color: "var(--warm)",
        fontSize: 13,
      }}
    >
      <span>
        Low balance: {formatBalance(balanceUSD)} left. Top up to keep your
        agents running.
      </span>
      <button
        type="button"
        onClick={onTopUp}
        style={{
          flexShrink: 0,
          height: 28,
          padding: "0 12px",
          borderRadius: "var(--r-2)",
          border: "1px solid rgba(255,181,71,0.45)",
          background: "transparent",
          color: "var(--warm)",
          fontSize: 12,
          fontWeight: 600,
          cursor: "pointer",
        }}
      >
        Top up
      </button>
    </div>
  );
}
