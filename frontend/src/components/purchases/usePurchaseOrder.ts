"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { purchaseAPI, type PurchaseOrder } from "@/lib/purchase";
import { safeOrderId, storedOrderKey } from "./format";

export function usePurchaseOrder(accountId: string | undefined, busy: boolean) {
  const [order, setOrder] = useState<PurchaseOrder | null>(null);
  const [stale, setStale] = useState(true);
  const [error, setError] = useState("");
  const [orderId, setOrderId] = useState<string | null>(null);
  const epoch = useRef(0);
  const active = useRef<{ account?: string; id: string | null }>({ id: null });

  const accept = useCallback((next: PurchaseOrder) => {
    if (!accountId || active.current.account !== accountId || !safeOrderId(next.id)) return;
    if (active.current.id && active.current.id !== next.id) return;
    active.current.id = next.id;
    setOrderId(next.id);
    setOrder(next);
    setStale(false);
    setError("");
    try { sessionStorage.setItem(storedOrderKey(accountId), next.id); } catch { /* The URL remains a recovery handle. */ }
    const url = new URL(window.location.href);
    url.searchParams.set("order", next.id);
    url.searchParams.delete("world");
    url.searchParams.delete("success");
    window.history.replaceState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
  }, [accountId]);

  const refresh = useCallback(async (id = active.current.id): Promise<PurchaseOrder | null> => {
    if (!id || !accountId || active.current.account !== accountId) return null;
    const ticket = ++epoch.current;
    setStale(true);
    try {
      const next = await purchaseAPI.getOrder(id);
      if (ticket !== epoch.current || active.current.account !== accountId || active.current.id !== id) return null;
      if (next.id !== id) throw new Error("The server returned a different order.");
      accept(next);
      return next;
    } catch {
      if (ticket === epoch.current && active.current.account === accountId) {
        setStale(true);
        setError("This order could not be refreshed. Check your connection and signed-in account, then retry.");
      }
      return null;
    }
  }, [accept, accountId]);

  const select = useCallback((next: PurchaseOrder) => {
    if (active.current.account !== accountId) return;
    epoch.current += 1;
    active.current.id = next.id;
    accept(next);
  }, [accept, accountId]);

  const invalidate = useCallback(() => {
    epoch.current += 1;
    setStale(true);
  }, []);

  useEffect(() => {
    active.current = { account: accountId, id: null };
    epoch.current += 1;
    let live = true;
    void Promise.resolve().then(async () => {
      if (!live) return;
      setOrder(null);
      setOrderId(null);
      setStale(true);
      setError("");
      if (!accountId) return;
      const fromURL = new URL(window.location.href).searchParams.get("order");
      let saved: string | null = null;
      try { saved = sessionStorage.getItem(storedOrderKey(accountId)); } catch { /* Storage may be disabled. */ }
      const id = safeOrderId(fromURL ?? saved);
      if (!id) {
        if (fromURL) setError("The order link is invalid. Choose a service to start a new order.");
        return;
      }
      active.current.id = id;
      setOrderId(id);
      await refresh(id);
    });
    return () => { live = false; active.current = { id: null }; epoch.current += 1; };
  }, [accountId, refresh]);

  useEffect(() => {
    if (!orderId || busy || ["completed", "denied", "cancelled"].includes(order?.state ?? "")) return;
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible") void refresh(orderId);
    }, 6000);
    const wake = () => { if (document.visibilityState === "visible") void refresh(orderId); };
    document.addEventListener("visibilitychange", wake);
    return () => { window.clearInterval(timer); document.removeEventListener("visibilitychange", wake); };
  }, [orderId, order?.state, busy, refresh]);

  return { order, orderId, stale, error, refresh, select, accept, invalidate };
}
