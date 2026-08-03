"use client";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { Topbar } from "@/components/Topbar";
import { bazaar, type BazaarResource } from "@/lib/bazaar";
import { ResourceCard } from "./ResourceCard";
import { AddToWorkflowDialog } from "./AddToWorkflowDialog";

const PAGE_SIZE = 30;

const GRID: React.CSSProperties = {
  display: "grid",
  gridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))",
  gap: 12,
};

export function BazaarPage() {
  const [supported, setSupported] = useState<BazaarResource[]>([]);
  const [items, setItems] = useState<BazaarResource[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [adding, setAdding] = useState<BazaarResource | null>(null);

  // The active search, debounced — separate from `query` so typing does not
  // fire a request per keystroke.
  const [activeQuery, setActiveQuery] = useState("");
  useEffect(() => {
    const t = window.setTimeout(() => setActiveQuery(query.trim()), 250);
    return () => window.clearTimeout(t);
  }, [query]);

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

  // Reset the paged list whenever the search changes. Done during render
  // (React's "adjusting state when a prop changes" pattern) rather than in a
  // useEffect, so the reset lands in the same commit as the query change
  // instead of firing a second, cascading render.
  const [resetForQuery, setResetForQuery] = useState(activeQuery);
  const [noMore, setNoMore] = useState(false);
  if (resetForQuery !== activeQuery) {
    setResetForQuery(activeQuery);
    setItems([]);
    setTotal(0);
    setNoMore(false);
  }

  // Bumped every time a search reset commits, so an in-flight loadMore
  // response from a stale (pre-reset) request can tell it's stale and skip
  // applying setTotal — items already self-guards via its own offset check,
  // but total has no equivalent natural staleness signal to compare against.
  // This must be useLayoutEffect, not useEffect: a plain useEffect is
  // deferred past paint via a macrotask, leaving a window (until that
  // macrotask runs) during which a stale request's already-queued
  // microtask continuation (the code right after `await bazaar.list(...)`)
  // could read the pre-bump value and slip through. useLayoutEffect runs
  // synchronously in the commit phase, closing that window entirely.
  const requestGeneration = useRef(0);
  useLayoutEffect(() => {
    requestGeneration.current += 1;
  }, [activeQuery]);

  const loadMore = useCallback(async () => {
    if (loading) return;
    setLoading(true);
    setError(null);
    const myGeneration = requestGeneration.current;
    try {
      const offset = items.length;
      const page = await bazaar.list({
        offset,
        limit: PAGE_SIZE,
        q: activeQuery || undefined,
        // The grid only shows unsupported entries — a supported one already
        // renders in the pinned section above, and showing it twice under
        // contradictory copy ("Community listings — you configure the fields
        // yourself") is actively wrong for a supported card.
        supported: false,
      });
      // A reset that happened while this request was in flight bumped the
      // generation counter — this response's total no longer describes the
      // current query, so skip it. items below has its own independent guard.
      if (requestGeneration.current === myGeneration) {
        setTotal(page.total);
        // A short page (fewer items than asked for) means there is nothing
        // more to fetch, regardless of what `total` says — `total` counts
        // the whole filtered set and can disagree with pagination in edge
        // cases (e.g. a concurrent catalog refresh), but a short page from
        // the backend is authoritative on its own.
        if (page.items.length < PAGE_SIZE) setNoMore(true);
      }
      // Guard against a concurrent reset landing between the request and its
      // response, which would otherwise append a stale page.
      setItems((cur) => (cur.length === offset ? [...cur, ...page.items] : cur));
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : "could not load the catalog");
    } finally {
      setLoading(false);
    }
  }, [items.length, loading, activeQuery]);

  // Sentinel-driven infinite scroll. Re-observes after every load so the
  // callback always closes over the current item count.
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return;
    // noMore (a short page) is the authoritative "stop fetching" signal — see
    // loadMore. Also gate on !error: a failed request leaves `loading` back
    // at false without ever marking noMore, and without this guard the
    // observer effect (re-created because loadMore's identity changed) would
    // immediately re-fire the same failing request forever. Auto-retry was
    // never the intent — the manual "Retry" button below is.
    if (noMore || loading || error) return;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) loadMore();
      },
      { rootMargin: "300px" },
    );
    io.observe(el);
    return () => io.disconnect();
  }, [loadMore, noMore, loading, error]);

  // Same signal the observer uses to stop fetching, so the UI's "done" state
  // and the fetch-stopping state can never disagree.
  const exhausted = noMore;

  return (
    <div style={{ minHeight: "100vh", display: "flex", flexDirection: "column" }}>
      <Topbar />
      <div style={{ padding: "24px 24px 64px", maxWidth: 1180, width: "100%", margin: "0 auto" }}>
        <h1 style={{ margin: 0, fontSize: 20, fontWeight: 600, color: "var(--fg)" }}>
          x402 Bazaar
        </h1>
        <p style={{ margin: "6px 0 0", fontSize: 13, color: "var(--fg-muted)", lineHeight: 1.6 }}>
          Every paid endpoint listed in GoPlausible&apos;s Algorand catalog. Add
          any of them to a workflow — supported ones are verified by
          AgentMesh, and arrive with hand-authored fields where we have them.
        </p>

        {supported.length > 0 && (
          <section style={{ marginTop: 24 }}>
            <SectionHeading title="Supported" note="Verified by AgentMesh." />
            <div style={GRID}>
              {supported.map((r) => (
                <ResourceCard key={r.id} resource={r} onAdd={setAdding} />
              ))}
            </div>
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
              title="All endpoints"
              note="Community listings — you configure the fields yourself."
            />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search endpoints…"
              aria-label="Search endpoints"
              style={{
                height: 32,
                padding: "0 10px",
                minWidth: 200,
                border: "1px solid var(--border)",
                background: "var(--bg)",
                borderRadius: "var(--r-2)",
                color: "var(--fg)",
                fontSize: 12.5,
                fontFamily: "var(--font-sans)",
              }}
            />
          </div>

          <div style={GRID}>
            {items.map((r) => (
              <ResourceCard key={r.id} resource={r} onAdd={setAdding} />
            ))}
          </div>

          {error && (
            <p style={{ marginTop: 16, fontSize: 12.5, color: "var(--danger)" }}>
              {error}{" "}
              <button
                type="button"
                onClick={loadMore}
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
            <p style={{ marginTop: 16, fontSize: 12.5, color: "var(--fg-dim)" }}>
              Loading…
            </p>
          )}

          {!loading && !error && items.length === 0 && activeQuery && (
            <p style={{ marginTop: 16, fontSize: 12.5, color: "var(--fg-dim)" }}>
              No endpoints match “{activeQuery}”.
            </p>
          )}

          {exhausted && items.length > 0 && (
            <p style={{ marginTop: 16, fontSize: 12, color: "var(--fg-dim)" }}>
              That&apos;s all {total} endpoints.
            </p>
          )}

          <div ref={sentinelRef} style={{ height: 1 }} />
        </section>
      </div>

      {adding && (
        <AddToWorkflowDialog resource={adding} onClose={() => setAdding(null)} />
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
