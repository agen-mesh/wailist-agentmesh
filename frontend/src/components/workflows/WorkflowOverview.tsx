"use client";

import { useRouter } from "next/navigation";
import { IconWallet } from "@/components/ui";
import { UpcomingRuns } from "@/components/runs/UpcomingRuns";
import { useCredits } from "@/lib/credits/store";
import styles from "./WorkflowOverview.module.css";

export function WorkflowOverview() {
  const router = useRouter();
  const { balanceUSD, balanceKnown } = useCredits();

  return (
    <div className={styles.overview}>
      <section className={styles.credit} aria-label="Credit balance">
        <div className={styles.creditHeading}>
          <span className={styles.creditLabel}>
            <IconWallet size={13} /> Credit balance
          </span>
          {/* Was a bare "+", which is a guess: plus WHAT. */}
          <button
            type="button"
            className={styles.creditAdd}
            onClick={() => router.push("/billing")}
          >
            <svg
              aria-hidden="true"
              width="13"
              height="13"
              viewBox="0 0 16 16"
              fill="none"
            >
              <path
                d="M8 3v10M3 8h10"
                stroke="currentColor"
                strokeWidth="1.75"
                strokeLinecap="round"
              />
            </svg>
            Add credits
          </button>
        </div>

        {/* One text node: the phone list renders this card too, and splitting
            the currency mark off breaks a plain getByText("$3.40") there. */}
        <p className={styles.creditBalance}>
          {balanceKnown ? (
            `$${balanceUSD.toFixed(2)}`
          ) : (
            <span className={styles.creditPlaceholder} />
          )}
        </p>
      </section>
      <UpcomingRuns limit={2} title="Upcoming runs" presentation="overview" />
    </div>
  );
}
