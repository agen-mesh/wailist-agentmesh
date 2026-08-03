"use client";
import { formatPrice, type BazaarResource } from "@/lib/bazaar";
import { EndpointRow } from "./EndpointRow";

const MAGENTA = "#E879F9";
const MAGENTA_SOFT = "rgba(232, 121, 249, 0.14)";

// Endpoints sharing a host collapse into one of these — one publisher can be
// 70%+ of the raw catalog, and a flat list of all of it at once buries
// everything else. Expands into a nested list of EndpointRows with a smooth
// height animation (CSS grid-template-rows 0fr->1fr, no JS measurement
// needed) rather than an instant show/hide, since the reveal is the one
// interaction this component exists to make pleasant.
export function ProviderGroupCard({
  host,
  resources,
  expanded,
  onToggle,
  onAdd,
}: {
  host: string;
  resources: BazaarResource[];
  expanded: boolean;
  onToggle: () => void;
  onAdd: (r: BazaarResource) => void;
}) {
  const label = resources[0]?.provider ?? host;
  const cheapest = Math.min(...resources.map((r) => r.amountMicros));

  return (
    <div>
      <button
        type="button"
        className="bz-row bz-row--group"
        aria-expanded={expanded}
        onClick={onToggle}
        style={{ width: "100%" }}
      >
        <span
          aria-hidden
          className="bz-row__icon bz-row__chevron"
          data-open={expanded}
          style={{ background: MAGENTA_SOFT, color: MAGENTA }}
        >
          ▸
        </span>
        <div style={{ minWidth: 0, flex: 1 }}>
          <span className="bz-row__name">{label}</span>
        </div>
        <div className="bz-row__meta">
          <span className="bz-row__stat">
            {resources.length} endpoint{resources.length === 1 ? "" : "s"}
          </span>
          <span className="bz-row__stat">from ${formatPrice(cheapest)}</span>
        </div>
      </button>

      <div className="bz-group-body" data-open={expanded}>
        <div className="bz-group-body__inner">
          {resources.map((r) => (
            <EndpointRow key={r.id} resource={r} onAdd={onAdd} indent />
          ))}
        </div>
      </div>
    </div>
  );
}
