"use client";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Topbar } from "@/components/Topbar";
import {
  bazaar,
  BAZAAR_SORT_OPTIONS,
  type BazaarResource,
  type BazaarSort,
} from "@/lib/bazaar";
import { ResourceCard } from "./ResourceCard";
import { ConsoleCard } from "./ConsoleCard";
import { EndpointRow } from "./EndpointRow";
import { ProviderGroupCard } from "./ProviderGroupCard";
import { AddToWorkflowDialog } from "./AddToWorkflowDialog";

// Real pagination, not infinite scroll: a fixed page is fetched and shown at
// a time, with Prev/Next and a page-size picker -- a long, unbounded list
// that keeps growing as you scroll was the exact complaint this replaces.
const PAGE_SIZE_OPTIONS = [5, 10, 20] as const;
type PageSize = (typeof PAGE_SIZE_OPTIONS)[number];
const DEFAULT_PAGE_SIZE: PageSize = 10;

// The partner track is explicit, not auto-fill. There are two partners; an
// auto-fill grid stretches to four columns on a wide screen and leaves them
// adrift in it, which reads as "two things are missing" rather than "these are
// the two".
//
// Flex rather than grid, and that is the whole point. A grid track count that
// does not divide the partner count leaves the last card alone beside dead
// space -- with three partners and room for two, the third sat in a half-empty
// row looking like a loading failure. Flex lets that last card GROW into the
// space instead (`flex: 1 1 <basis>` on the card itself), so a row is always
// full whatever the count and the viewport. The basis sets the point at which
// another card fits; nothing is ever stranded.
const CONSOLE_GRID: React.CSSProperties = {
  display: "flex",
  flexWrap: "wrap",
  alignItems: "stretch",
  gap: 14,
};

// Any supported entry WITHOUT a console still renders as an ordinary card.
// Nothing produces one today (curated.go's TestEveryCuratedEntryIsConsoleBacked
// requires a console key), but a registry that grows a non-console partner
// should degrade to a card rather than vanish from the page.
const GRID: React.CSSProperties = {
  display: "grid",
  gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))",
  gap: 12,
};

// Prev/Next share one style, differing only in their disabled state -- a
// function rather than two near-duplicate inline style objects.
function paginationBtnStyle(disabled: boolean): React.CSSProperties {
  return {
    height: 32,
    padding: "0 14px",
    background: "var(--bg-elev-1)",
    border: "1px solid var(--border)",
    borderRadius: "var(--r-2)",
    color: "var(--fg-muted)",
    fontFamily: "var(--font-sans)",
    fontSize: 12.5,
    cursor: disabled ? "default" : "pointer",
    opacity: disabled ? 0.45 : 1,
  };
}

// The community list renders as one bordered row-list rather than a card
// grid: a card grid puts variable-height cards into CSS grid cells (no
// masonry), which produced a visibly broken, ragged layout once entries
// with different description lengths sat next to each other. A uniform-
// height row list has no such alignment problem, and reads as the same
// "real API directory" language as the rest of the page's dense data.
const BAZAAR_CSS = `
.bz-list {
  border: 1px solid var(--border);
  border-radius: var(--r-3);
  overflow: hidden;
  background: var(--bg-elev-1);
}
.bz-row {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  padding: 13px 16px;
  border: none;
  border-bottom: 1px solid var(--border);
  background: transparent;
  text-align: left;
  font-family: var(--font-sans);
  color: inherit;
  transition: background 0.15s var(--ease);
}
.bz-row--group {
  cursor: pointer;
}
.bz-row::before {
  content: "";
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 2px;
  background: var(--accent);
  transform: scaleY(0);
  transition: transform 0.15s var(--ease);
}
/* Every row hovers the same way, not just the expandable group headers --
   a plain row is just as much a target (its own Add button) and reading a
   long list is easier when the row under the pointer is visually obvious,
   not only the ones that happen to expand. */
.bz-row:hover,
.bz-row:focus-within {
  background: var(--bg-elev-2);
}
.bz-row:hover::before,
.bz-row:focus-within::before {
  transform: scaleY(1);
}
.bz-row__icon {
  width: 26px;
  height: 26px;
  border-radius: var(--r-2);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  flex-shrink: 0;
}
.bz-row__chevron {
  transition: transform 0.2s var(--ease);
}
.bz-row__chevron[data-open="true"] {
  transform: rotate(90deg);
}
.bz-row__name {
  font-size: 13px;
  font-weight: 600;
  color: var(--fg);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.bz-row__path {
  font-family: var(--font-mono);
  font-size: 10.5px;
  color: var(--fg-dim);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex-shrink: 1;
  min-width: 0;
}
.bz-row__desc {
  margin: 2px 0 0;
  font-size: 11.5px;
  color: var(--fg-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.bz-row__meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--fg-dim);
}
.bz-row__price {
  color: var(--fg);
  font-weight: 600;
}
.bz-row__price-unit {
  margin-right: 2px;
}
.bz-row__stat {
  white-space: nowrap;
}
.bz-pill {
  padding: 1px 6px;
  border-radius: var(--r-1);
  background: var(--bg-elev-3);
  border: 1px solid var(--border-strong);
  font-size: 10px;
}
.bz-row__add {
  flex-shrink: 0;
  height: 26px;
  padding: 0 12px;
  border: 1px solid var(--border-strong);
  background: transparent;
  color: var(--fg);
  border-radius: var(--r-2);
  font-size: 11.5px;
  font-weight: 500;
  font-family: var(--font-sans);
  cursor: pointer;
  transition:
    background 0.15s var(--ease),
    border-color 0.15s var(--ease),
    color 0.15s var(--ease);
}
.bz-row__add:hover {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--accent-fg);
}
.bz-group-body {
  display: grid;
  grid-template-rows: 0fr;
  transition: grid-template-rows 0.22s var(--ease);
}
.bz-group-body[data-open="true"] {
  grid-template-rows: 1fr;
}
.bz-group-body__inner {
  overflow: hidden;
  min-height: 0;
  background: var(--bg-elev-2);
}
.bz-supported-card {
  /* Fill the grid cell. Grid stretches this wrapper to the tallest card in
     the row, but the card inside is intrinsically sized, so a short
     description would otherwise leave a visible gap under it and the row
     would look ragged. */
  height: 100%;
}
.bz-supported-card > * {
  height: 100%;
}
/* The search box and sort select, next to the "Everything else" heading.
   flex-wrap alone was not enough: a flex item's default min-width is its
   own CONTENT width, not 0, so the input's fixed 200px floor plus the
   select's own intrinsic width (its longest option, "Price: low to high")
   could together exceed a narrow container's remaining space and overflow
   past the edge instead of shrinking -- the classic flexbox gotcha. min-width
   0 here is what actually lets them shrink; the media query is what gives a
   genuinely narrow (phone-width) screen a clean full-width stack instead of
   two squeezed controls sharing a cramped row. */
.bz-toolbar {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.bz-toolbar input,
.bz-toolbar select {
  flex: 1 1 150px;
  min-width: 0;
}
@media (max-width: 480px) {
  .bz-toolbar {
    width: 100%;
  }
  .bz-toolbar input,
  .bz-toolbar select {
    flex: 1 1 100%;
  }
}
@media (prefers-reduced-motion: reduce) {
  .bz-row,
  .bz-row::before,
  .bz-row__chevron,
  .bz-group-body {
    transition: none !important;
  }
}
`;

export function BazaarPage() {
  const [supported, setSupported] = useState<BazaarResource[]>([]);
  const [items, setItems] = useState<BazaarResource[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [adding, setAdding] = useState<BazaarResource | null>(null);
  const [pageSize, setPageSize] = useState<PageSize>(DEFAULT_PAGE_SIZE);
  const [page, setPage] = useState(0); // 0-indexed

  // Supported entries collapse by console key: Prism is four endpoints behind
  // one page, and four cards all opening that same page would be four buttons
  // for one destination. Entries without a console key keep one card each.
  // Map preserves first-seen order, so a console surfaces where its first
  // endpoint would have.
  const { consoles, plainSupported } = useMemo(() => {
    const byConsole = new Map<string, BazaarResource[]>();
    const plain: BazaarResource[] = [];
    for (const r of supported) {
      if (!r.console) {
        plain.push(r);
        continue;
      }
      const list = byConsole.get(r.console);
      if (list) list.push(r);
      else byConsole.set(r.console, [r]);
    }
    return { consoles: Array.from(byConsole.entries()), plainSupported: plain };
  }, [supported]);

  // Endpoints sharing a host collapse into one ProviderGroupCard so a single
  // heavy publisher doesn't bury everything else on the page (one host is
  // over 70% of the raw catalog). Grouping is client-side over just the
  // current page's `items` -- with real pagination that's at most 20 rows,
  // trivial to group per render. A host's other entries can land on a
  // different page than this one; that's an ordinary, expected pagination
  // outcome now; see ProviderGroupCard's partial={false} below.
  const groupedItems = useMemo(() => {
    const byHost = new Map<string, BazaarResource[]>();
    for (const r of items) {
      const list = byHost.get(r.host);
      if (list) list.push(r);
      else byHost.set(r.host, [r]);
    }
    return Array.from(byHost.entries());
  }, [items]);
  const [expandedHosts, setExpandedHosts] = useState<Set<string>>(new Set());
  const toggleHost = useCallback((host: string) => {
    setExpandedHosts((cur) => {
      const next = new Set(cur);
      if (next.has(host)) next.delete(host);
      else next.add(host);
      return next;
    });
  }, []);

  // The active search, debounced — separate from `query` so typing does not
  // fire a request per keystroke.
  const [activeQuery, setActiveQuery] = useState("");
  useEffect(() => {
    const t = window.setTimeout(() => setActiveQuery(query.trim()), 250);
    return () => window.clearTimeout(t);
  }, [query]);

  // "Most used" (settles) is the crawl's own default order and needs no
  // param at all. Meaningless while a search is active — bazaar.list()
  // already drops it server-side whenever q is set, since match relevance
  // is a more useful order than a client-picked one — so the control below
  // disables itself in that state instead of quietly doing nothing.
  const [sort, setSort] = useState<BazaarSort>("settles");

  // Supported entries are pinned above the scrolling list, so they are fetched
  // once and never paged. Searching does not filter them — the point of the
  // section is that it is always visible.
  useEffect(() => {
    let cancelled = false;
    // supported: true asks the backend to filter over the FULL merged catalog,
    // not just the first 100-by-settle-count entries — a curated entry with
    // zero catalog matches (e.g. Tendril) is appended past that cutoff, so a
    // client-side .filter() over one page could silently miss it.
    bazaar
      .list({ offset: 0, limit: 100, supported: true })
      .then((page) => {
        if (!cancelled) setSupported(page.items);
      })
      .catch(() => {
        /* the main list surfaces the error; a missing pinned row is not fatal */
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Jump back to page 0 whenever the search, sort, or page size changes --
  // a new view of the list starts over, rather than e.g. landing on "page 3"
  // of a search that only has one page of results. Done during render
  // (React's "adjusting state when a prop changes" pattern) rather than in a
  // useEffect, so the reset lands in the same commit as the change instead
  // of firing a second, cascading render.
  const viewKey = `${activeQuery} ${sort} ${pageSize}`;
  const [resetForKey, setResetForKey] = useState(viewKey);
  if (resetForKey !== viewKey) {
    setResetForKey(viewKey);
    setPage(0);
  }

  // retryTick has no meaning of its own -- bumping it is just how the Retry
  // button below re-triggers a fetch of the same page without duplicating
  // the fetch effect below.
  const [retryTick, setRetryTick] = useState(0);

  // Marks loading the instant the fetch's own inputs change, in the SAME
  // render/commit as that change -- same "adjusting state" pattern as the
  // page reset above, and for the same reason: setting it from inside the
  // effect below instead would still be correct, but is a synchronous
  // setState at the top of an effect body, which is exactly the extra,
  // avoidable cascading-render round trip that pattern exists to skip (and
  // what react-hooks/set-state-in-effect flags). fetchKey intentionally
  // reads `page` AFTER the reset above has had a chance to land it at 0, so
  // a query/sort/size change that also resets the page is only ever one
  // fetch, not two.
  const fetchKey = `${page}|${pageSize}|${activeQuery}|${sort}|${retryTick}`;
  const [fetchKeyForLoading, setFetchKeyForLoading] = useState(fetchKey);
  if (fetchKeyForLoading !== fetchKey) {
    setFetchKeyForLoading(fetchKey);
    setLoading(true);
    setError(null);
  }

  // Fetches exactly one page and REPLACES items, rather than the old
  // infinite-scroll accumulator that appended forever -- real pagination
  // means the request itself changes (offset/limit) on every page/size/sort/
  // search/retry change, so a plain effect keyed on all five, with a
  // `cancelled` guard against a stale response landing after a newer
  // request already resolved, is simpler than the accumulator ever needed
  // to be.
  useEffect(() => {
    let cancelled = false;
    bazaar
      .list({
        offset: page * pageSize,
        limit: pageSize,
        q: activeQuery || undefined,
        sort,
        // The grid only shows unsupported entries -- a supported one already
        // renders in the pinned section above, and showing it twice under
        // contradictory copy ("Community listings -- you configure the
        // fields yourself") is actively wrong for a supported card.
        supported: false,
      })
      .then((res) => {
        if (cancelled) return;
        setItems(res.items);
        setTotal(res.total);
      })
      .catch((e: unknown) => {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : "could not load the catalog");
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [page, pageSize, activeQuery, sort, retryTick]);

  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const canGoPrev = page > 0;
  const canGoNext = page < totalPages - 1;

  return (
    // .am-viewport-min rather than a raw 100vh: on a phone browser 100vh
    // measures the viewport with the toolbars retracted, so the page runs taller
    // than the screen and its bottom sits behind the address bar. The class
    // carries the vh -> dvh fallback pair, which an inline style cannot express.
    // min-height, not height, because this list grows as it pages in.
    <div
      className="am-viewport-min"
      style={{ display: "flex", flexDirection: "column" }}
    >
      <style>{BAZAAR_CSS}</style>
      <Topbar />
      <div
        style={{
          padding: "24px 24px 64px",
          maxWidth: 1180,
          width: "100%",
          margin: "0 auto",
        }}
      >
        <h1
          style={{
            margin: 0,
            fontSize: 26,
            fontWeight: 600,
            letterSpacing: "-0.02em",
            color: "var(--fg)",
          }}
        >
          x402 Bazaar
        </h1>
        <p
          style={{
            margin: "8px 0 0",
            fontSize: 13.5,
            color: "var(--fg-muted)",
            lineHeight: 1.65,
            maxWidth: "68ch",
          }}
        >
          Services your agents can pay for by the call, no accounts or API keys
          needed. Our partners come with a ready-made page. Everything else you
          can drop straight onto a canvas.
        </p>

        {supported.length > 0 && (
          <section style={{ marginTop: 28 }}>
            <SectionHeading
              title="Partners"
              note="Set up and tested by us. Open one and start using it right away."
            />
            {consoles.length > 0 && (
              <div style={CONSOLE_GRID}>
                {consoles.map(([key, resources]) => (
                  <ConsoleCard
                    key={key}
                    consoleKey={key}
                    resources={resources}
                  />
                ))}
              </div>
            )}
            {plainSupported.length > 0 && (
              <div style={{ ...GRID, marginTop: consoles.length > 0 ? 12 : 0 }}>
                {plainSupported.map((r) => (
                  <div key={r.id} className="bz-supported-card">
                    <ResourceCard resource={r} onAdd={setAdding} />
                  </div>
                ))}
              </div>
            )}
          </section>
        )}

        <section style={{ marginTop: 28 }}>
          <div
            style={{
              display: "flex",
              alignItems: "baseline",
              justifyContent: "space-between",
              gap: 12,
              flexWrap: "wrap",
              marginBottom: 12,
            }}
          >
            <SectionHeading
              title="Everything else"
              note="Public listings. Add one to a canvas and fill in its details yourself."
            />
            <div className="bz-toolbar">
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search…"
                aria-label="Search endpoints"
                style={{
                  height: 32,
                  padding: "0 10px",
                  border: "1px solid var(--border)",
                  background: "var(--bg)",
                  borderRadius: "var(--r-2)",
                  color: "var(--fg)",
                  fontSize: 12.5,
                  fontFamily: "var(--font-sans)",
                }}
              />
              <select
                value={sort}
                onChange={(e) => setSort(e.target.value as BazaarSort)}
                disabled={Boolean(activeQuery)}
                aria-label="Sort endpoints"
                title={
                  activeQuery
                    ? "Search results are already ordered by best match."
                    : undefined
                }
                style={{
                  height: 32,
                  padding: "0 8px",
                  border: "1px solid var(--border)",
                  background: activeQuery ? "var(--bg-elev-2)" : "var(--bg)",
                  borderRadius: "var(--r-2)",
                  color: activeQuery ? "var(--fg-dim)" : "var(--fg)",
                  fontSize: 12.5,
                  fontFamily: "var(--font-sans)",
                  cursor: activeQuery ? "default" : "pointer",
                }}
              >
                {BAZAAR_SORT_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </div>
          </div>

          <div className="bz-list">
            {groupedItems.map(([host, resources]) =>
              resources.length === 1 ? (
                <EndpointRow
                  key={resources[0].id}
                  resource={resources[0]}
                  onAdd={setAdding}
                />
              ) : (
                <ProviderGroupCard
                  key={host}
                  host={host}
                  resources={resources}
                  expanded={expandedHosts.has(host)}
                  onToggle={() => toggleHost(host)}
                  onAdd={setAdding}
                  partial={false}
                />
              ),
            )}
          </div>

          {error && (
            <p
              style={{ marginTop: 16, fontSize: 12.5, color: "var(--danger)" }}
            >
              {error}{" "}
              <button
                type="button"
                onClick={() => setRetryTick((t) => t + 1)}
                style={{
                  background: "none",
                  border: "none",
                  color: "var(--accent)",
                  cursor: "pointer",
                  fontSize: 12.5,
                  textDecoration: "underline",
                  fontFamily: "var(--font-sans)",
                }}
              >
                Retry
              </button>
            </p>
          )}

          {loading && (
            <p
              style={{ marginTop: 16, fontSize: 12.5, color: "var(--fg-dim)" }}
            >
              Loading…
            </p>
          )}

          {!loading && !error && items.length === 0 && activeQuery && (
            <p
              style={{ marginTop: 16, fontSize: 12.5, color: "var(--fg-dim)" }}
            >
              Nothing matches “{activeQuery}”.
            </p>
          )}

          {!loading && !error && items.length > 0 && (
            <div
              style={{
                marginTop: 16,
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                gap: 12,
                flexWrap: "wrap",
              }}
            >
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 6,
                  fontSize: 12,
                  color: "var(--fg-dim)",
                }}
              >
                <span>Show</span>
                <select
                  value={pageSize}
                  onChange={(e) =>
                    setPageSize(Number(e.target.value) as PageSize)
                  }
                  aria-label="Results per page"
                  style={{
                    height: 28,
                    padding: "0 6px",
                    border: "1px solid var(--border)",
                    background: "var(--bg)",
                    borderRadius: "var(--r-2)",
                    color: "var(--fg)",
                    fontSize: 12,
                    fontFamily: "var(--font-sans)",
                    cursor: "pointer",
                  }}
                >
                  {PAGE_SIZE_OPTIONS.map((n) => (
                    <option key={n} value={n}>
                      {n}
                    </option>
                  ))}
                </select>
                <span>per page · {total} total</span>
              </div>

              <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
                <span style={{ fontSize: 12, color: "var(--fg-dim)" }}>
                  Page {page + 1} of {totalPages}
                </span>
                <button
                  type="button"
                  onClick={() => setPage((p) => Math.max(0, p - 1))}
                  disabled={!canGoPrev}
                  style={paginationBtnStyle(!canGoPrev)}
                >
                  ← Prev
                </button>
                <button
                  type="button"
                  onClick={() => setPage((p) => p + 1)}
                  disabled={!canGoNext}
                  style={paginationBtnStyle(!canGoNext)}
                >
                  Next →
                </button>
              </div>
            </div>
          )}
        </section>
      </div>

      {adding && (
        <AddToWorkflowDialog
          resource={adding}
          onClose={() => setAdding(null)}
        />
      )}
    </div>
  );
}

function SectionHeading({ title, note }: { title: string; note: string }) {
  return (
    <div style={{ marginBottom: 10 }}>
      <div
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 10,
          textTransform: "uppercase",
          letterSpacing: "0.08em",
          color: "var(--fg-dim)",
        }}
      >
        {title}
      </div>
      <div style={{ fontSize: 12, color: "var(--fg-muted)", marginTop: 3 }}>
        {note}
      </div>
    </div>
  );
}
