"use client";
import { useCredits } from "@/lib/credits/store";

// Reactive low-balance warning driven by the mock wallet: shows when the balance
// drops below the auto-recharge threshold. Mock only — no real recharge occurs.
export function LowBalanceBanner({ onTopUp }: { onTopUp: () => void }) {
  const { balanceUSD, autoRecharge, hydrated } = useCredits();

  if (!hydrated || balanceUSD >= autoRecharge.thresholdUSD) return null;

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
        Low balance: ${balanceUSD.toFixed(2)} left.
        {autoRecharge.enabled
          ? " Auto-recharge is on."
          : " Top up to keep your agents running."}
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
