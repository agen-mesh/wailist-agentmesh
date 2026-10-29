"use client";

import { useCallback, useEffect, useState, type FormEvent } from "react";
import { connectProviderWallet, inspectProviderWallet, publishOffer, OfferWriteError, type InputSchema, type PublishedOffer, type WalletState } from "@/lib/arkiv";
import { purchaseAPI } from "@/lib/purchase";
import type { BrowserWallet } from "./wallet";
import { toMicros } from "./format";
import styles from "./Purchases.module.css";

interface Props {
  provider: BrowserWallet | null;
  walletRevision: number;
  rpcURL?: string;
  disabled: boolean;
  simulation: boolean;
  onWalletChange: () => void;
  onPublished: (key: string) => Promise<boolean>;
}

export function ProviderOfferForm({ provider, walletRevision, rpcURL, disabled, simulation, onWalletChange, onPublished }: Props) {
  const [wallet, setWallet] = useState<WalletState>({ status: "disconnected", message: "Connect a provider wallet to publish an offer." });
  const [capability, setCapability] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [price, setPrice] = useState("");
  const [version, setVersion] = useState("1");
  const [deadline, setDeadline] = useState("");
  const [expiry, setExpiry] = useState("");
  const [schema, setSchema] = useState('{"type":"object","properties":{},"required":[],"additionalProperties":false}');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [uncertain, setUncertain] = useState(false);
  const [published, setPublished] = useState<PublishedOffer | null>(null);
  const [discovered, setDiscovered] = useState(false);

  const inspect = useCallback(async () => {
    const next = await inspectProviderWallet(provider ?? undefined, rpcURL);
    setWallet(next);
  }, [provider, rpcURL]);

  useEffect(() => {
    let live = true;
    void inspectProviderWallet(provider ?? undefined, rpcURL).then((state) => { if (live) setWallet(state); });
    return () => { live = false; };
  }, [provider, rpcURL, walletRevision]);

  async function connect() {
    setBusy(true); setError("");
    try {
      const next = await connectProviderWallet(provider ?? undefined, rpcURL);
      setWallet(next);
      if (!expiry) setExpiry((BigInt(next.currentBlock) + 1000n).toString());
      onWalletChange();
    } catch (err) { setError(err instanceof Error ? err.message : "Provider wallet connection failed."); }
    finally { setBusy(false); }
  }

  async function publish(event: FormEvent) {
    event.preventDefault();
    if (wallet.status !== "ready" || busy || disabled || uncertain) return;
    setBusy(true); setError(""); setPublished(null); setDiscovered(false);
    try {
      const seconds = Math.floor(Date.parse(deadline) / 1000);
      if (!Number.isSafeInteger(seconds) || seconds <= Math.floor(Date.now() / 1000)) throw new Error("Choose a future offer deadline.");
      const inputSchema = JSON.parse(schema) as InputSchema;
      const result = await publishOffer({
        provider: provider ?? undefined, rpcURL, expectedAccount: wallet.account,
        offer: { capability: capability.trim(), priceMicros: toMicros(price), version, validUntil: seconds.toString(), expiresAtBlock: expiry,
          payload: { endpoint: endpoint.trim(), method: "POST", payee: wallet.account, input_schema: inputSchema } },
        validateEndpoint: async (url) => { await purchaseAPI.preflightOfferEndpoint(url); },
      });
      setPublished(result);
      setDiscovered(await onPublished(result.entityKey));
    } catch (err) {
      if (err instanceof OfferWriteError && err.code === "write-uncertain") setUncertain(true);
      setError(err instanceof Error ? err.message : "Offer publication could not be confirmed.");
    } finally { setBusy(false); await inspect(); }
  }

  return <section className={styles.card} aria-label="Publish a provider offer">
    <details className={styles.details}>
      <summary>Publish a service offer</summary>
      <p className={styles.muted}>Your provider wallet owns the public Arkiv listing and receives the Arc payment at the same address. Listing ownership does not certify merchant reliability.</p>
      <p className={styles.notice} role="status">{wallet.status === "ready" ? `Arkiv Tiramisu connected. Current block ${wallet.currentBlock}.` : wallet.message}</p>
      {wallet.status === "ready" && <p className={styles.mono}>Provider / payee: {wallet.account}</p>}
      <button className={styles.secondary} type="button" onClick={() => void connect()} disabled={busy || disabled || !provider || !rpcURL}>Connect provider wallet on Tiramisu</button>
      <p className={styles.muted}>Publishing uses test GLM. Switching to Tiramisu changes the connected wallet network; customer payments require Arc testnet.</p>
      {simulation && <p className={styles.warning}>The app is in simulation mode. Any explicitly submitted Arkiv wallet transaction is a separate testnet write, not a simulated service payment.</p>}
      <form onSubmit={(event) => void publish(event)}>
        <fieldset disabled={busy || disabled || wallet.status !== "ready" || uncertain} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
          <label className={styles.field}>Capability<input className={styles.input} required pattern="[a-z][a-z0-9_.-]{0,63}" maxLength={64} value={capability} onChange={(event) => setCapability(event.target.value)} placeholder="text-summary" /></label>
          <label className={styles.field}>Public HTTPS service endpoint<input className={styles.input} required type="url" value={endpoint} onChange={(event) => setEndpoint(event.target.value)} placeholder="https://service.example/purchase" /></label>
          <div className={styles.row}>
            <label className={styles.field}>Price in USDC<input className={styles.input} required inputMode="decimal" value={price} onChange={(event) => setPrice(event.target.value)} placeholder="0.01" /></label>
            <label className={styles.field}>Offer version<input className={styles.input} required inputMode="numeric" value={version} onChange={(event) => setVersion(event.target.value)} /></label>
          </div>
          <label className={styles.field}>Valid until (your local time)<input className={styles.input} required type="datetime-local" value={deadline} onChange={(event) => setDeadline(event.target.value)} /></label>
          <label className={styles.field}>Expires at Arkiv block<input className={styles.input} required inputMode="numeric" value={expiry} onChange={(event) => setExpiry(event.target.value)} /></label>
          <label className={styles.field}>Public input schema<textarea className={styles.textarea} required value={schema} onChange={(event) => setSchema(event.target.value)} spellCheck={false} /></label>
          <p className={styles.muted}>Publish field names and scalar types only. Customer inputs, examples, credentials and identity data stay out of the public entity. Additional properties must be disabled.</p>
          <button className={styles.button} type="submit">{busy ? "Confirming offer write..." : "Publish with provider wallet"}</button>
        </fieldset>
      </form>
      {error && <p className={styles.error} role="alert">{error}</p>}
      {uncertain && <p className={styles.warning}>The write may have been submitted. Inspect wallet activity and refresh discovery before publishing another offer.</p>}
      {published && <div className={styles.notice} role="status">
        <p>Arkiv write confirmed. {discovered ? "The offer was also returned by discovery." : "Discovery has not returned this offer yet. Refresh the listing to check it."}</p>
        <p className={styles.mono}>Entity: {published.entityKey}</p><p className={styles.mono}>Transaction: {published.txHash}</p>
        <p>Expiry block: {published.expiresAtBlock}</p>
      </div>}
    </details>
  </section>;
}
