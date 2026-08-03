"use client";
import { Pill } from "@/components/ui";
import { formatPrice, type BazaarResource } from "@/lib/bazaar";

// The tool402 accent, matching the canvas node and Inspector so an endpoint
// looks like the same thing everywhere it appears (Inspector.tsx:276-280).
const MAGENTA = "#E879F9";
const MAGENTA_SOFT = "rgba(232, 121, 249, 0.14)";

// One catalog entry. A supported entry is visually distinct because the badge
// is the whole promise of the page: it means we have hand-authored this
// endpoint's fields and verified a real settlement through it.
export function ResourceCard({
  resource,
  onAdd,
}: {
  resource: BazaarResource;
  onAdd: (r: BazaarResource) => void;
}) {
  const paramCount = resource.params.length;
  return (
    <div
      style={{
        border: `1px solid ${resource.supported ? "var(--accent-line)" : "var(--border)"}`,
        background: resource.supported ? "var(--accent-soft)" : "var(--bg-elev-1)",
        borderRadius: "var(--r-2)",
        padding: 14,
        display: "flex",
        flexDirection: "column",
        gap: 10,
        minWidth: 0,
      }}
    >
      <div style={{ display: "flex", alignItems: "flex-start", gap: 10 }}>
        <span
          aria-hidden
          style={{
            width: 26,
            height: 26,
            borderRadius: "var(--r-2)",
            background: MAGENTA_SOFT,
            color: MAGENTA,
            display: "inline-flex",
            alignItems: "center",
            justifyContent: "center",
            fontSize: 13,
            flexShrink: 0,
          }}
        >
          ✦
        </span>
        <div style={{ minWidth: 0, flex: 1 }}>
          <div
            style={{
              fontSize: 13,
              fontWeight: 600,
              color: "var(--fg)",
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
          >
            {resource.provider ?? resource.host}
          </div>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 10.5,
              color: "var(--fg-dim)",
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
            }}
            title={resource.url}
          >
            {resource.method} {new URL(resource.url).pathname}
          </div>
        </div>
        {resource.supported && <Pill tone="warm">Supported</Pill>}
      </div>

      <p
        style={{
          margin: 0,
          fontSize: 12,
          lineHeight: 1.55,
          color: "var(--fg-muted)",
          display: "-webkit-box",
          WebkitLineClamp: 3,
          WebkitBoxOrient: "vertical",
          overflow: "hidden",
        }}
      >
        {resource.description || "No description published."}
      </p>

      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 8,
          flexWrap: "wrap",
          fontSize: 11,
          color: "var(--fg-dim)",
          fontFamily: "var(--font-mono)",
        }}
      >
        <span style={{ color: "var(--fg)", fontWeight: 600 }}>
          ${formatPrice(resource.amountMicros)}
        </span>
        <span>/ call</span>
        {resource.testnet && <Pill>testnet</Pill>}
        {resource.settleCount > 0 && <span>· {resource.settleCount} settles</span>}
        {paramCount > 0 && (
          <span>
            · {paramCount} field{paramCount === 1 ? "" : "s"}
          </span>
        )}
      </div>

      {/* An unsupported endpoint is usable, but nothing about its input shape
          is guaranteed — say so rather than letting a blank node imply it is
          ready to run. */}
      {!resource.supported && (
        <div style={{ fontSize: 11, color: "var(--fg-dim)", lineHeight: 1.5 }}>
          Community listing — you&apos;ll configure its fields yourself after adding.
        </div>
      )}

      <button
        type="button"
        onClick={() => onAdd(resource)}
        style={{
          height: 32,
          border: `1px solid ${resource.supported ? "var(--accent)" : "var(--border-strong)"}`,
          background: resource.supported ? "var(--accent)" : "transparent",
          color: resource.supported ? "var(--accent-fg)" : "var(--fg)",
          borderRadius: "var(--r-2)",
          fontSize: 12,
          fontWeight: 500,
          cursor: "pointer",
          fontFamily: "var(--font-sans)",
        }}
      >
        Add to workflow
      </button>
    </div>
  );
}
