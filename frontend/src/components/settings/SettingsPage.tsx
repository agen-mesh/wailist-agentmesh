"use client";
import { useCallback, useEffect, useState } from "react";
import { Topbar } from "@/components/Topbar";
import { useAuth } from "@/hooks/useAuth";
import {
  settings as settingsApi,
  type UserSettings,
  type UserSettingsPatch,
} from "@/lib/api";
import { setLowBalanceThresholdUSD } from "@/lib/credits/store";
import { AccountSection } from "@/components/settings/sections/Account";
import { BillingSection } from "@/components/settings/sections/Billing";
import { ExecutionSection } from "@/components/settings/sections/Execution";
import { DeveloperSection } from "@/components/settings/sections/Developer";
import { ConnectionsSection } from "@/components/settings/sections/Connections";
import { panelStyle } from "@/components/settings/ui";

const SETTINGS_CSS = `
.set-grid { display: grid; grid-template-columns: 168px minmax(0, 1fr); gap: 28px; align-items: start; }
.set-rail { position: sticky; top: 0; display: grid; gap: 2px; }
.set-sections { display: grid; gap: 16px; }
.set-section { animation: fade-up 0.35s var(--ease) both; }
.set-rail-link {
  text-align: left; padding: 6px 10px; border: none; border-radius: var(--r-2);
  background: transparent; color: var(--fg-muted); font-family: var(--font-sans);
  font-size: 12.5px; cursor: pointer;
  transition: background .15s var(--ease), color .15s var(--ease);
}
.set-rail-link:hover { background: var(--bg-elev-2); color: var(--fg); }
.set-save { transition: transform .12s var(--ease), box-shadow .2s var(--ease); }
.set-save:not(:disabled):hover { box-shadow: 0 10px 28px var(--accent-glow); }
.set-save:not(:disabled):active { transform: scale(0.97); }
.set-copy:active { transform: scale(0.97); }
.set-status { animation: fade-in 0.2s var(--ease) both; }
/* The rail only duplicates in-page headings, so it is the part that goes when
   there is no room for two columns. */
@media (max-width: 900px) { .set-grid { grid-template-columns: minmax(0, 1fr); } .set-rail { display: none; } }
@media (prefers-reduced-motion: reduce) {
  .set-section, .set-status { animation: none; }
  .set-rail-link, .set-save, .set-copy { transition: none; }
  .set-save:not(:disabled):active, .set-copy:active { transform: none; }
}
`;

// Placeholder shown while a section's data is still in flight. Deliberately a
// quiet line of text rather than a shimmering block: these panels resolve in
// one request, and a skeleton that flashes for 150ms is more distracting than
// the thing it stands in for.
function SectionSkeleton({ label }: { label: string }) {
  return (
    <div
      style={{ ...panelStyle, fontSize: 13, color: "var(--fg-dim)" }}
      aria-busy="true"
    >
      {label}
    </div>
  );
}

const RAIL = [
  { id: "account", label: "Account" },
  { id: "password", label: "Password" },
  { id: "billing", label: "Billing" },
  { id: "execution", label: "Execution" },
  { id: "connections", label: "Connections" },
  { id: "developer", label: "Developer" },
];

export function SettingsPage() {
  const { user, loading: userLoading, completeOnboarding } = useAuth();
  const [settings, setSettings] = useState<UserSettings | null>(null);
  const [loadError, setLoadError] = useState("");

  useEffect(() => {
    let live = true;
    settingsApi
      .get()
      .then((s) => {
        if (!live) return;
        setSettings(s);
        // Keep the client-side credit store in step, so the low-balance banner
        // and the canvas indicator use the saved threshold immediately rather
        // than the built-in default until the next reload.
        setLowBalanceThresholdUSD(s.lowBalanceUsdMicros / 1e6);
      })
      .catch((e: unknown) =>
        setLoadError(
          e instanceof Error ? e.message : "Could not load your settings.",
        ),
      );
    return () => {
      live = false;
    };
  }, []);

  const save = useCallback(async (patch: UserSettingsPatch) => {
    // The server returns the full merged row, so state is replaced with what
    // was actually stored rather than with what we hoped we sent.
    const saved = await settingsApi.update(patch);
    setSettings(saved);
    if (patch.lowBalanceUsdMicros !== undefined) {
      setLowBalanceThresholdUSD(saved.lowBalanceUsdMicros / 1e6);
    }
  }, []);

  const jumpTo = (id: string) => {
    document
      .getElementById(id)
      ?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  return (
    <div
      style={{
        height: "100vh",
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
        background: "var(--bg)",
      }}
    >
      <style>{SETTINGS_CSS}</style>
      <Topbar />

      <div style={{ flex: 1, overflow: "auto", background: "var(--bg)" }}>
        <div
          style={{
            maxWidth: 1040,
            margin: "0 auto",
            padding: "40px 24px 96px",
          }}
        >
          <header style={{ marginBottom: 24 }}>
            <h1
              style={{
                fontSize: 26,
                fontWeight: 700,
                letterSpacing: "-0.02em",
                margin: 0,
                color: "var(--fg)",
              }}
            >
              Settings
            </h1>
            <p
              style={{
                maxWidth: "60ch",
                fontSize: 13,
                lineHeight: 1.55,
                color: "var(--fg-muted)",
                margin: "6px 0 0",
              }}
            >
              Your account, spending guardrails, and the endpoints your agents
              run behind.
            </p>
          </header>

          <div className="set-grid">
            <nav className="set-rail" aria-label="Settings sections">
              {RAIL.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  className="set-rail-link"
                  onClick={() => jumpTo(item.id)}
                >
                  {item.label}
                </button>
              ))}
            </nav>

            <div className="set-sections">
              {loadError && (
                <div
                  role="alert"
                  style={{
                    ...panelStyle,
                    borderColor: "var(--danger)",
                    color: "var(--danger)",
                    fontSize: 13,
                  }}
                >
                  {loadError}
                </div>
              )}

              {/* Gated on a resolved user, not rendered optimistically with a
                  null one. AccountSection seeds its form state from these
                  fields with useState, which ignores later prop changes — so
                  mounting before /auth/me lands leaves the name and org inputs
                  permanently blank, and saving from that state would overwrite
                  the stored organisation with an empty string. */}
              {userLoading && <SectionSkeleton label="Loading your account…" />}
              {user && (
                <AccountSection
                  user={user}
                  onProfileSaved={completeOnboarding}
                />
              )}

              {/* Same reasoning as the account gate above: both sections seed
                  useState from `settings`, so they must not mount before it
                  arrives. */}
              {!settings && !loadError && (
                <SectionSkeleton label="Loading your settings…" />
              )}
              {settings && (
                <>
                  <BillingSection settings={settings} onSave={save} />
                  <ExecutionSection settings={settings} onSave={save} />
                </>
              )}

              <ConnectionsSection />
              <DeveloperSection />
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
