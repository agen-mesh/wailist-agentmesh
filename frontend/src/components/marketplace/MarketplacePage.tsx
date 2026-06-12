"use client";
import { useState, useMemo, useEffect, useCallback, useRef } from "react";
import { useRouter } from "next/navigation";
import { Logo, Pill, Tag, IconSearch, Toast } from "@/components/ui";
import { MARKETPLACE_ENDPOINTS } from "@/lib/data";
import type { MarketplaceEndpoint } from "@/lib/types";
import { marketplace as marketplaceApi } from "@/lib/api";
import { WorkflowPickerModal } from "./WorkflowPickerModal";

const CATEGORIES = [
  { id: "all",     label: "All" },
  { id: "search",  label: "Search" },
  { id: "data",    label: "Data" },
  { id: "ai",      label: "AI" },
  { id: "finance", label: "Finance" },
  { id: "media",   label: "Media" },
  { id: "util",    label: "Util" },
] as const;
type CategoryId = (typeof CATEGORIES)[number]["id"];


export function MarketplacePage() {
  const router = useRouter();
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState<CategoryId>("all");
  const [uploadOpen, setUploadOpen] = useState(false);
  const [toast, setToast] = useState<string | null>(null);

  const [pickerEndpoint, setPickerEndpoint] = useState<MarketplaceEndpoint | null>(null);
  const [bazaarEndpoints, setBazaarEndpoints] = useState<MarketplaceEndpoint[]>([]);
  const [bazaarLoading, setBazaarLoading] = useState(true);
  const [bazaarError, setBazaarError] = useState(false);
  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const showToast = (msg: string) => { setToast(msg); setTimeout(() => setToast(null), 2600); };

  const showFeatured = category === "all" && !query;

  // Initial load
  useEffect(() => {
    setBazaarLoading(true);
    marketplaceApi
      .bazaarList(24, 0)
      .then(({ endpoints }) => { setBazaarEndpoints(endpoints); setBazaarError(false); })
      .catch(() => setBazaarError(true))
      .finally(() => setBazaarLoading(false));
  }, []);

  // Debounced search
  useEffect(() => {
    if (!query.trim()) {
      marketplaceApi.bazaarList(24, 0).then(({ endpoints }) => setBazaarEndpoints(endpoints)).catch(() => {});
      return;
    }
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => {
      marketplaceApi
        .bazaarSearch(query)
        .then(({ endpoints }) => setBazaarEndpoints(endpoints))
        .catch(() => {});
    }, 400);
    return () => { if (searchTimer.current) clearTimeout(searchTimer.current); };
  }, [query]);

  // static entries filtered by category + query (client-side)
  const filteredStatic = useMemo(() =>
    MARKETPLACE_ENDPOINTS.filter((ep) => {
      const matchCat = category === "all" || ep.category === category;
      const q = query.toLowerCase();
      const matchQ = !q || ep.name.toLowerCase().includes(q) || ep.description.toLowerCase().includes(q) || ep.tags.some((t) => t.includes(q));
      return matchCat && matchQ;
    }), [query, category]);

  // Bazaar entries filtered by category only (search is server-side)
  const filteredBazaar = useMemo(() =>
    bazaarEndpoints.filter((ep) =>
      category === "all" || ep.category === category
    ), [bazaarEndpoints, category]);

  const handleAdd = (ep: MarketplaceEndpoint) => {
    if (ep.endpoint) {
      setPickerEndpoint(ep);
    } else {
      showToast(`${ep.name} noted — set the endpoint in the inspector after adding`);
    }
  };

  return (
    <div style={{ minHeight: "100vh", background: "var(--bg)", display: "flex", flexDirection: "column" }}>
      {/* ── Nav ── */}
      <header style={{ height: 52, flexShrink: 0, background: "var(--bg-elev-1)", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", padding: "0 24px", gap: 16 }}>
        <button onClick={() => router.push("/")} style={ghostStyle}><Logo size={16} /></button>
        <div style={{ width: 1, height: 20, background: "var(--border)" }} />
        <button onClick={() => router.push("/workflows")} style={navLinkStyle}>Workflows</button>
        <span style={{ color: "var(--fg)", fontSize: 13, fontWeight: 500 }}>Marketplace</span>
        <div style={{ flex: 1 }} />
        <button onClick={() => router.push("/billing")} style={navLinkStyle}>Billing</button>
        <button onClick={() => setUploadOpen(true)} style={primaryBtnStyle}>+ Publish</button>
      </header>

      {/* ── Hero ── */}
      <div style={{ padding: "48px 24px 32px", textAlign: "center", borderBottom: "1px solid var(--border)", background: "linear-gradient(180deg, var(--bg-elev-1) 0%, var(--bg) 100%)" }}>
        <div style={{ marginBottom: 12 }}><Tag>x402 Marketplace</Tag></div>
        <h1 style={{ margin: 0, fontSize: 32, fontWeight: 700, letterSpacing: "-0.03em", color: "var(--fg)", lineHeight: 1.2 }}>
          Plug-and-pay AI tools &amp; workflows
        </h1>
        <p style={{ margin: "12px auto 0", maxWidth: 520, color: "var(--fg-muted)", fontSize: 15, lineHeight: 1.7 }}>
          Browse x402-enabled endpoints and ready-made workflows. Every tool is pay-per-call — no API keys, no subscriptions.
        </p>
        <div style={{ display: "flex", alignItems: "center", gap: 10, maxWidth: 480, margin: "24px auto 0", background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: "var(--r-2)", padding: "0 14px", height: 40 }}>
          <IconSearch size={14} />
          <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search Bazaar endpoints…"
            style={{ flex: 1, background: "transparent", border: "none", outline: "none", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)" }} />
        </div>
      </div>

      {/* ── Content ── */}
      <div style={{ flex: 1, maxWidth: 1120, margin: "0 auto", width: "100%", padding: "0 24px 48px" }}>
        {/* Category chips */}
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginTop: 24, marginBottom: 28 }}>
          {CATEGORIES.map((cat) => (
            <button key={cat.id} onClick={() => setCategory(cat.id)} style={{
              height: 28, padding: "0 12px", fontSize: 12, fontWeight: 500, borderRadius: 999, cursor: "pointer",
              fontFamily: "var(--font-sans)",
              background: category === cat.id ? "var(--accent-soft)" : "var(--bg-elev-2)",
              border: `1px solid ${category === cat.id ? "var(--accent-line)" : "var(--border)"}`,
              color: category === cat.id ? "var(--accent)" : "var(--fg-muted)",
            }}>{cat.label}</button>
          ))}
        </div>

        {/* Curated static section */}
        {showFeatured && filteredStatic.filter((e) => e.featured).length > 0 && (
          <div style={{ marginBottom: 36 }}>
            <SectionLabel>Curated</SectionLabel>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
              {filteredStatic.filter((e) => e.featured).map((ep) => (
                <EndpointCard key={ep.id} ep={ep} featured onAdd={() => handleAdd(ep)} />
              ))}
            </div>
          </div>
        )}

        {/* Live Bazaar section */}
        <div style={{ marginBottom: 36 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 14 }}>
            <SectionLabel>{query ? `Bazaar results · ${filteredBazaar.length}` : "Live from Bazaar"}</SectionLabel>
            {bazaarLoading && <span style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>fetching…</span>}
            {bazaarError && <span style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: "#f87171" }}>bazaar unreachable</span>}
          </div>
          {bazaarLoading && (
            <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
              {Array.from({ length: 6 }).map((_, i) => <SkeletonCard key={i} />)}
            </div>
          )}
          {!bazaarLoading && filteredBazaar.length === 0 && !bazaarError && (
            <div style={{ fontSize: 12, color: "var(--fg-dim)", fontFamily: "var(--font-mono)", padding: "24px 0" }}>
              {query ? `No Bazaar results for "${query}"` : "No endpoints found"}
            </div>
          )}
          {!bazaarLoading && filteredBazaar.length > 0 && (
            <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 16 }}>
              {filteredBazaar.map((ep) => (
                <EndpointCard key={ep.id} ep={ep} onAdd={() => handleAdd(ep)} />
              ))}
            </div>
          )}
        </div>
      </div>

      {pickerEndpoint && (
        <WorkflowPickerModal endpoint={pickerEndpoint} onClose={() => setPickerEndpoint(null)} />
      )}
      {uploadOpen && <UploadModal onClose={() => setUploadOpen(false)} onSubmit={(name) => { setUploadOpen(false); showToast(`"${name}" submitted for review`); }} />}
      {toast && <Toast message={toast} />}
    </div>
  );
}

// ── Helpers ───────────────────────────────────────────────────────────────
function SectionLabel({ children }: { children: React.ReactNode }) {
  return <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", textTransform: "uppercase", letterSpacing: "0.08em", marginBottom: 14 }}>{children}</div>;
}

function SkeletonCard() {
  return (
    <div style={{ background: "var(--bg-elev-1)", border: "1px solid var(--border)", borderRadius: "var(--r-3)", padding: "18px 20px", height: 140, display: "flex", flexDirection: "column", gap: 10 }}>
      <div style={{ display: "flex", gap: 12, alignItems: "flex-start" }}>
        <div style={{ width: 40, height: 40, borderRadius: "var(--r-2)", background: "var(--bg-elev-3)" }} />
        <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 6 }}>
          <div style={{ height: 12, width: "60%", borderRadius: 4, background: "var(--bg-elev-3)" }} />
          <div style={{ height: 10, width: "40%", borderRadius: 4, background: "var(--bg-elev-3)" }} />
        </div>
      </div>
      <div style={{ height: 10, width: "90%", borderRadius: 4, background: "var(--bg-elev-3)" }} />
      <div style={{ height: 10, width: "70%", borderRadius: 4, background: "var(--bg-elev-3)" }} />
    </div>
  );
}

function EmptyState({ query }: { query: string }) {
  return <div style={{ textAlign: "center", padding: "48px 0", color: "var(--fg-dim)", fontFamily: "var(--font-mono)", fontSize: 13 }}>No results{query ? ` for "${query}"` : ""}</div>;
}

// ── Endpoint Card ─────────────────────────────────────────────────────────
function EndpointCard({ ep, featured = false, onAdd }: { ep: MarketplaceEndpoint; featured?: boolean; onAdd: () => void }) {
  const [hovered, setHovered] = useState(false);
  return (
    <div
      onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)}
      style={{ background: featured ? "var(--bg-elev-2)" : "var(--bg-elev-1)", border: `1px solid ${hovered ? "var(--accent-line)" : featured ? "var(--border-strong)" : "var(--border)"}`, borderRadius: "var(--r-3)", padding: "18px 20px", display: "flex", flexDirection: "column", gap: 12, transition: "border-color 0.15s" }}
    >
      <div style={{ display: "flex", alignItems: "flex-start", gap: 12 }}>
        <div style={{ width: 40, height: 40, borderRadius: "var(--r-2)", background: "rgba(232,121,249,0.12)", border: "1px solid rgba(232,121,249,0.25)", display: "flex", alignItems: "center", justifyContent: "center", fontSize: 18, flexShrink: 0 }}>{ep.icon}</div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 3 }}>
            <span style={{ fontSize: 14, fontWeight: 600, color: "var(--fg)" }}>{ep.name}</span>
            {featured && <Pill tone="accent">Featured</Pill>}
            {ep.source === "bazaar" && <Pill tone="accent">Bazaar</Pill>}
          </div>
          <div style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>{ep.provider}</div>
        </div>
        <div style={{ textAlign: "right", flexShrink: 0 }}>
          <div style={{ fontSize: 14, fontWeight: 700, color: "#E879F9" }}>${ep.price}</div>
          <div style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>per {ep.unit}</div>
        </div>
      </div>
      <p style={{ margin: 0, fontSize: 12, color: "var(--fg-muted)", lineHeight: 1.6 }}>{ep.description}</p>
      <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
        {ep.tags.map((t) => <span key={t} style={{ fontSize: 10, fontFamily: "var(--font-mono)", color: "var(--fg-dim)", background: "var(--bg-elev-3)", borderRadius: "var(--r-1)", padding: "2px 7px", border: "1px solid var(--border)" }}>{t}</span>)}
      </div>
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginTop: 4 }}>
        <div style={{ display: "flex", gap: 14 }}>
          <span style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--fg-dim)" }}>⟳ {(ep.calls / 1000).toFixed(0)}k</span>
          <span style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--warm)" }}>★ {ep.rating}</span>
        </div>
        <button onClick={onAdd} style={ghostBtnStyle}>+ Add to workflow</button>
      </div>
    </div>
  );
}

// ── Upload Modal ──────────────────────────────────────────────────────────
function UploadModal({ onClose, onSubmit }: { onClose: () => void; onSubmit: (name: string) => void }) {
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [price, setPrice] = useState("");
  const [desc, setDesc] = useState("");
  const valid = name.trim().length > 2 && url.trim().length > 4;

  return (
    <div style={{ position: "fixed", inset: 0, background: "rgba(8,7,12,0.7)", backdropFilter: "blur(4px)", zIndex: 100, display: "flex", alignItems: "center", justifyContent: "center" }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div style={{ width: 500, background: "var(--bg-elev-2)", border: "1px solid var(--border-strong)", borderRadius: "var(--r-3)", padding: "28px", display: "flex", flexDirection: "column", gap: 18 }}>
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
          <div>
            <div style={{ fontSize: 15, fontWeight: 600, color: "var(--fg)" }}>Publish to Marketplace</div>
            <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--fg-dim)", marginTop: 3 }}>Share your x402 endpoint with the community</div>
          </div>
          <button onClick={onClose} style={{ background: "transparent", border: "none", color: "var(--fg-muted)", cursor: "pointer", fontSize: 18 }}>✕</button>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
          <ModalField label="Name" value={name} onChange={setName} placeholder="e.g. NewsAPI Search" />
          <ModalField label="Endpoint URL" value={url} onChange={setUrl} placeholder="https://your-api.com/endpoint" />
          <ModalField label="Price per call (USD)" value={price} onChange={setPrice} placeholder="0.005" />
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            <label style={{ fontSize: 12, fontWeight: 500, color: "var(--fg-muted)", fontFamily: "var(--font-sans)" }}>Description</label>
            <textarea value={desc} onChange={(e) => setDesc(e.target.value)} placeholder="What does this do? What inputs does it take?" style={{ width: "100%", minHeight: 80, padding: "8px 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)", resize: "vertical", outline: "none", lineHeight: 1.6, boxSizing: "border-box" }} />
          </div>
        </div>
        <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
          <button onClick={onClose} style={ghostBtnStyle}>Cancel</button>
          <button onClick={() => valid && onSubmit(name)} disabled={!valid} style={{ ...primaryBtnStyle, opacity: valid ? 1 : 0.5, cursor: valid ? "pointer" : "default" }}>Submit for review</button>
        </div>
      </div>
    </div>
  );
}

function ModalField({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (v: string) => void; placeholder: string }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
      <label style={{ fontSize: 12, fontWeight: 500, color: "var(--fg-muted)", fontFamily: "var(--font-sans)" }}>{label}</label>
      <input value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} style={{ height: 36, padding: "0 12px", background: "var(--bg)", border: "1px solid var(--border)", borderRadius: "var(--r-2)", color: "var(--fg)", fontSize: 13, fontFamily: "var(--font-sans)", outline: "none" }} />
    </div>
  );
}

const ghostStyle: React.CSSProperties = { background: "transparent", border: "none", cursor: "pointer", padding: 0, display: "inline-flex" };
const navLinkStyle: React.CSSProperties = { background: "transparent", border: "none", cursor: "pointer", fontSize: 13, color: "var(--fg-muted)", fontFamily: "var(--font-sans)", padding: "4px 6px" };
const primaryBtnStyle: React.CSSProperties = { height: 32, padding: "0 16px", fontSize: 12, fontWeight: 600, background: "var(--accent)", border: "1px solid var(--accent)", borderRadius: "var(--r-2)", color: "var(--accent-fg)", cursor: "pointer", fontFamily: "var(--font-sans)", display: "inline-flex", alignItems: "center", gap: 4 };
const ghostBtnStyle: React.CSSProperties = { height: 32, padding: "0 14px", fontSize: 12, fontWeight: 500, background: "transparent", border: "1px solid var(--border-strong)", borderRadius: "var(--r-2)", color: "var(--fg-muted)", cursor: "pointer", fontFamily: "var(--font-sans)" };
