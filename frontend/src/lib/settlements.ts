"use client";
import type { Settlement } from "@/lib/types";

// Local settlement history, recorded from run logs as they complete.
//
// This exists because the server cannot answer the question yet:
// x402_relay_settlements has no user_id column (the relay is a public,
// unauthenticated route, so at insert time there is no user to attribute a
// settlement to), and returning unscoped rows would show one user another
// user's payments. Until that table gains an owner column, the client keeps
// its own copy of what it saw its own runs do.
//
// Keyed per user id: signing in as someone else must not surface the previous
// account's settlements from the same browser.

const PREFIX = "agentmesh_settlements_";
// Roughly a few hundred KB at this row size — plenty of history without
// crowding out other localStorage users.
const MAX_ROWS = 300;

function key(userId: string): string {
  return PREFIX + userId;
}

export function listSettlements(userId: string | null): Settlement[] {
  if (!userId || typeof window === "undefined") return [];
  try {
    const raw = window.localStorage.getItem(key(userId));
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? (parsed as Settlement[]) : [];
  } catch {
    return [];
  }
}

// recordSettlements merges new rows in, newest first, de-duplicated by txId.
// A run's transcript can be re-read (reopened workflow, replayed stream, a
// poll that overlaps the live events), so the same settlement can legitimately
// arrive more than once and must not appear twice.
//
// De-duplication spans the incoming batch itself, not just what is already
// stored: a run-funded agent settles ONE real inbound payment up front and
// every tool call it then makes reports that same tx id (see the backend's
// executeTool402RunLevel), so a single run's transcript legitimately contains
// several rows for one settlement. The first occurrence wins, which is the
// run-funding row the backend emits ahead of the per-call receipts — the one
// carrying the full amount that actually moved, rather than one call's slice.
export function recordSettlements(
  userId: string | null,
  incoming: Settlement[],
): void {
  if (!userId || typeof window === "undefined" || incoming.length === 0) return;
  try {
    const existing = listSettlements(userId);
    const seen = new Set(existing.map((s) => s.txId).filter(Boolean));
    const fresh = incoming.filter((s) => {
      if (!s.txId || seen.has(s.txId)) return false;
      seen.add(s.txId);
      return true;
    });
    if (fresh.length === 0) return;
    const merged = [...fresh, ...existing].slice(0, MAX_ROWS);
    window.localStorage.setItem(key(userId), JSON.stringify(merged));
  } catch {
    /* quota or unavailable storage: history is best-effort */
  }
}
