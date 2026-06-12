"use client";
import { useRouter } from "next/navigation";
import { Logo, Tag, Pill } from "@/components/ui";

export function BillingPage() {
  const router = useRouter();
  return (
    <div style={{ minHeight: "100vh", background: "var(--bg)", display: "flex", flexDirection: "column" }}>
      {/* ── Nav ── */}
      <header style={{ height: 52, flexShrink: 0, background: "var(--bg-elev-1)", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", padding: "0 24px", gap: 16 }}>
        <button onClick={() => router.push("/")} style={{ background: "transparent", border: "none", cursor: "pointer", padding: 0, display: "inline-flex" }}><Logo size={16} /></button>
        <div style={{ width: 1, height: 20, background: "var(--border)" }} />
        <button onClick={() => router.push("/workflows")} style={navLinkStyle}>Workflows</button>
        <button onClick={() => router.push("/marketplace")} style={navLinkStyle}>Marketplace</button>
        <span style={{ color: "var(--fg)", fontSize: 13, fontWeight: 500 }}>Billing</span>
        <div style={{ flex: 1 }} />
      </header>

      {/* ── Hero ── */}
      <div style={{ padding: "60px 24px 48px", textAlign: "center", borderBottom: "1px solid var(--border)" }}>
        <div style={{ marginBottom: 14 }}><Tag>Pricing</Tag></div>
        <h1 style={{ margin: 0, fontSize: 36, fontWeight: 700, letterSpacing: "-0.03em", color: "var(--fg)", lineHeight: 1.2 }}>
          Pay only for what you run
        </h1>
        <p style={{ margin: "14px auto 0", maxWidth: 500, color: "var(--fg-muted)", fontSize: 15, lineHeight: 1.7 }}>
          No seats, no tiers, no minimums. AgentMesh charges you for actual compute — LLM tokens and x402 tool calls your agents make.
        </p>
      </div>

      <div style={{ maxWidth: 860, margin: "0 auto", width: "100%", padding: "48px 24px 80px" }}>
        {/* ── Plan cards ── */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 20, marginBottom: 56 }}>
          <PlanCard
            name="Credits" tagline="Prepay and never get surprised"
            price="$10" priceSub="minimum top-up"
            accent="var(--accent)" highlight={false}
            cta="Buy Credits" onCta={() => alert("Coming soon — join the waitlist to be notified")}
            features={["Buy credit bundles starting at $10", "Credits never expire", "Instant top-up via card or crypto", "Spending dashboard with per-workflow breakdown", "Set per-workflow budget caps to avoid overruns", "Works across all LLM providers and x402 tools"]}
            note="Best for: regular users who run workflows on a schedule and want predictable spend."
          />
          <PlanCard
            name="Pay-per-use" tagline="Run now, pay at month end"
            price="$0" priceSub="upfront — billed monthly"
            accent="#E879F9" highlight
            cta="Enable Postpaid" onCta={() => alert("Coming soon — postpaid is in closed beta")}
            features={["No upfront payment required", "Run workflows immediately", "Aggregated invoice sent on the 1st of each month", "Line-item breakdown per workflow and run", "Pay by card, bank transfer, or ALGO", "Spending alerts at 80% and 100% of your monthly estimate"]}
            note="Best for: teams exploring automation or running bursty, unpredictable workloads."
          />
        </div>

        {/* ── How charges work ── */}
        <SectionLabel>How charges work</SectionLabel>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16, marginBottom: 56 }}>
          {[
            { step: "01", title: "LLM token costs", body: "Each agent node calls an LLM provider. We pass through costs at the provider's published rate with no markup. The canvas cost estimator shows the expected cost before you run." },
            { step: "02", title: "x402 tool calls",  body: "Tool402 nodes pay the endpoint operator directly using your Algorand agent wallet, at the listed price. The amount is settled on-chain instantly — no billing delay." },
            { step: "03", title: "Infrastructure",   body: "Workflow execution, storage, and SSE streaming are included free. You only pay for the AI work your agents perform, not for using the platform." },
          ].map((c) => (
            <div key={c.step} style={{ background: "var(--bg-elev-1)", border: "1px solid var(--border)", borderRadius: "var(--r-3)", padding: "20px 22px" }}>
              <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--accent)", marginBottom: 10 }}>{c.step}</div>
              <div style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)", marginBottom: 8 }}>{c.title}</div>
              <div style={{ fontSize: 12, color: "var(--fg-muted)", lineHeight: 1.65 }}>{c.body}</div>
            </div>
          ))}
        </div>

        {/* ── FAQ ── */}
        <SectionLabel>Common questions</SectionLabel>
        <div style={{ border: "1px solid var(--border)", borderRadius: "var(--r-3)", overflow: "hidden" }}>
          {[
            { q: "What happens when my credits run out?",       a: "Workflows will pause and you'll receive an email. You can top up instantly — there's no queue or wait. Switching to postpaid means this can never happen." },
            { q: "Can I mix credits and postpaid?",             a: "Not currently — you pick one billing mode per account. You can switch modes at any time from this page; the change takes effect at the start of the next billing cycle." },
            { q: "How are x402 payments settled?",              a: "x402 tool calls are paid from each agent's Algorand wallet, funded by your AgentMesh credits or charged to your postpaid account. Either way, the on-chain transaction is instant." },
            { q: "Is there a free tier?",                       a: "You get $2 of free credits when you sign up — enough for thousands of lightweight runs. No credit card required to start." },
            { q: "Where can I see a full cost breakdown?",      a: "The workflow canvas shows a per-run estimate. After each run, the log drawer shows actual token counts and x402 payments. Full history is in the dashboard under Billing → Usage." },
          ].map((item, i, arr) => (
            <div key={i} style={{ padding: "18px 22px", borderBottom: i < arr.length - 1 ? "1px solid var(--border)" : "none" }}>
              <div style={{ fontSize: 13, fontWeight: 600, color: "var(--fg)", marginBottom: 6 }}>{item.q}</div>
              <div style={{ fontSize: 12, color: "var(--fg-muted)", lineHeight: 1.65 }}>{item.a}</div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.08em", marginBottom: 20 }}>{children}</div>;
}

function PlanCard({ name, tagline, price, priceSub, accent, highlight, cta, onCta, features, note }: {
  name: string; tagline: string; price: string; priceSub: string;
  accent: string; highlight: boolean;
  cta: string; onCta: () => void;
  features: string[]; note: string;
}) {
  return (
    <div style={{ background: highlight ? "var(--bg-elev-2)" : "var(--bg-elev-1)", border: `1px solid ${highlight ? accent + "50" : "var(--border)"}`, borderRadius: "var(--r-3)", padding: "28px 24px", display: "flex", flexDirection: "column", gap: 20, position: "relative", overflow: "hidden" }}>
      {highlight && <div style={{ position: "absolute", top: 0, left: 0, right: 0, height: 2, background: `linear-gradient(90deg, transparent, ${accent}, transparent)` }} />}
      <div>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 6 }}>
          <span style={{ fontSize: 16, fontWeight: 700, color: "var(--fg)" }}>{name}</span>
          {highlight && <Pill tone="accent">Popular</Pill>}
        </div>
        <div style={{ fontSize: 12, color: "var(--fg-muted)" }}>{tagline}</div>
      </div>
      <div>
        <span style={{ fontSize: 36, fontWeight: 800, color: accent, letterSpacing: "-0.04em" }}>{price}</span>
        <span style={{ fontSize: 12, color: "var(--fg-dim)", marginLeft: 8 }}>{priceSub}</span>
      </div>
      <button onClick={onCta} style={{ height: 38, width: "100%", fontSize: 13, fontWeight: 600, background: highlight ? accent : "var(--bg-elev-3)", border: `1px solid ${highlight ? accent : "var(--border-strong)"}`, borderRadius: "var(--r-2)", cursor: "pointer", color: highlight ? "#100A1E" : "var(--fg)", fontFamily: "var(--font-sans)" }}>{cta}</button>
      <div style={{ height: 1, background: "var(--border)" }} />
      <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        {features.map((f, i) => (
          <div key={i} style={{ display: "flex", gap: 10, alignItems: "flex-start" }}>
            <span style={{ color: accent, fontSize: 13, flexShrink: 0, marginTop: 1, fontWeight: 700 }}>✓</span>
            <span style={{ fontSize: 13, color: "var(--fg-muted)", lineHeight: 1.5 }}>{f}</span>
          </div>
        ))}
      </div>
      <div style={{ fontSize: 11, color: "var(--fg-dim)", background: "var(--bg)", borderRadius: "var(--r-1)", padding: "10px 12px", lineHeight: 1.6, fontStyle: "italic" }}>{note}</div>
    </div>
  );
}

const navLinkStyle: React.CSSProperties = { background: "transparent", border: "none", cursor: "pointer", fontSize: 13, color: "var(--fg-muted)", fontFamily: "var(--font-sans)", padding: "4px 6px" };
