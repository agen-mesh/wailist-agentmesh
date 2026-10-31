"use client";

import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import Link from "next/link";
import { Topbar } from "@/components/Topbar";
import { useAuth } from "@/hooks/useAuth";
import { purchaseAPI, type PurchaseAllowance, type PurchaseConfig, type PurchaseOffer, type PurchaseOrder, type PurchaseWallet } from "@/lib/purchase";
import { ARKIV_CHAIN_ID } from "@/lib/arkiv/schema";
import { ARC_CHAIN, assertAuthorizationMatches, expired, immutableOrder, offerUnavailable, toMicros, usdc } from "./format";
import { browserWallet, checkWallet, signPurchase, signWalletChallenge, switchWalletChain, walletAccount, walletChain, type BrowserWallet } from "./wallet";
import { usePurchaseOrder } from "./usePurchaseOrder";
import { OrderPanel } from "./OrderPanel";
import { ProviderOfferForm } from "./ProviderOfferForm";
import styles from "./Purchases.module.css";

function failure(error: unknown): string {
  return error instanceof Error ? error.message.slice(0, 500) : "The operation could not be confirmed. Refresh before retrying.";
}

export function PurchasesPage() {
  const auth = useAuth();
  const [config, setConfig] = useState<PurchaseConfig | null>(null);
  const [configError, setConfigError] = useState("");
  const [offers, setOffers] = useState<PurchaseOffer[]>([]);
  const [offersLoading, setOffersLoading] = useState(true);
  const [offersError, setOffersError] = useState("");
  const [wallet, setWallet] = useState<PurchaseWallet | null>(null);
  const [allowance, setAllowance] = useState<PurchaseAllowance | null>(null);
  const [allowanceError, setAllowanceError] = useState("");
  const [accountError, setAccountError] = useState("");
  const [provider, setProvider] = useState<BrowserWallet | null>(null);
  const [account, setAccount] = useState("");
  const [chain, setChain] = useState<number | null>(null);
  const [walletRevision, setWalletRevision] = useState(0);
  const [walletError, setWalletError] = useState("");
  const [selected, setSelected] = useState<PurchaseOffer | null>(null);
  const [requestText, setRequestText] = useState("{}");
  const [maxCost, setMaxCost] = useState("");
  const [limit, setLimit] = useState("");
  const [capability, setCapability] = useState("");
  const [priceFilter, setPriceFilter] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [now, setNow] = useState(() => Date.now());
  const locked = useRef(false);
  const accountEpoch = useRef(0);
  const allowanceRequest = useRef(0);
  const requestId = useRef<{ terms: string; id: string } | null>(null);
  const current = usePurchaseOrder(auth.user?.id, Boolean(busy));

  const refreshAllowance = useCallback(async (resetLimit = false) => {
    const epoch = accountEpoch.current;
    const ticket = ++allowanceRequest.current;
    setAllowance(null); setAllowanceError("");
    try {
      const budget = await purchaseAPI.allowance();
      if (epoch !== accountEpoch.current || ticket !== allowanceRequest.current) return;
      setAllowance(budget);
      if (resetLimit) setLimit(usdc(budget.limitMicros));
    } catch {
      if (epoch !== accountEpoch.current || ticket !== allowanceRequest.current) return;
      setAllowanceError("Your spending limit could not be refreshed. Use Refresh account to check the server ledger.");
    }
  }, []);

  const inspectWallet = useCallback(async () => {
    const next = browserWallet();
    setProvider(next);
    setWalletRevision((value) => value + 1);
    if (!next) { setAccount(""); setChain(null); return; }
    try {
      const address = await walletAccount(next);
      const network = await walletChain(next);
      setAccount(address); setChain(network); setWalletError("");
    } catch {
      setAccount(""); setChain(null);
      setWalletError("Wallet disconnected or unavailable. Connect it before signing.");
    }
  }, []);

  useEffect(() => {
    let live = true;
    const attached = browserWallet();
    const changed = () => { if (live) void inspectWallet(); };
    void Promise.resolve().then(changed);
    attached?.on?.("accountsChanged", changed);
    attached?.on?.("chainChanged", changed);
    attached?.on?.("disconnect", changed);
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => {
      live = false;
      window.clearInterval(timer);
      attached?.removeListener?.("accountsChanged", changed);
      attached?.removeListener?.("chainChanged", changed);
      attached?.removeListener?.("disconnect", changed);
    };
  }, [inspectWallet]);

  useEffect(() => {
    let live = true;
    accountEpoch.current += 1;
    void Promise.resolve().then(async () => {
      if (!live) return;
      setConfig(null); setWallet(null); setAllowance(null); setOffers([]); setSelected(null);
      setError(""); setAccountError(""); setAllowanceError(""); setOffersError(""); setConfigError("");
      setOffersLoading(true); setLimit(""); requestId.current = null;
      if (!auth.user?.id) return;
      const [settings, listing, linked] = await Promise.allSettled([
        purchaseAPI.config(), purchaseAPI.offers(), purchaseAPI.wallet(),
      ]);
      if (!live) return;
      if (settings.status === "fulfilled") setConfig(settings.value);
      else setConfigError("Purchase services are unavailable. No payment actions can start.");
      if (listing.status === "fulfilled") setOffers(listing.value.offers);
      else setOffersError("Live service discovery could not be read. Retry to check current offers.");
      if (linked.status === "fulfilled") {
        if (linked.value) await refreshAllowance(true);
        if (!live) return;
        setWallet(linked.value);
      }
      else setAccountError("Your linked payment wallet could not be confirmed.");
      setOffersLoading(false);
    });
    return () => { live = false; accountEpoch.current += 1; };
  }, [auth.user?.id, refreshAllowance]);

  useEffect(() => {
    if (!wallet?.address || !current.order?.signatureReleasedAt) return;
    let live = true;
    void Promise.resolve().then(() => { if (live) void refreshAllowance(); });
    return () => { live = false; };
  }, [wallet?.address, current.order?.id, current.order?.state, current.order?.signatureReleasedAt,
    current.order?.txHash, current.order?.paymentEvent?.logIndex, refreshAllowance]);

  async function run(label: string, action: (isCurrent: () => boolean) => Promise<void>) {
    if (locked.current || !auth.signedIn) return;
    const epoch = accountEpoch.current;
    const isCurrent = () => epoch === accountEpoch.current;
    locked.current = true; setBusy(label); setError("");
    try { await action(isCurrent); }
    catch (err) { if (isCurrent()) setError(failure(err)); }
    finally { locked.current = false; setBusy(""); }
  }

  async function refreshOffers(publishedKey?: string): Promise<boolean> {
    const epoch = accountEpoch.current;
    setOffersLoading(true); setOffersError("");
    try {
      const result = await purchaseAPI.offers(publishedKey ? {} : {
        capability: capability.trim() || undefined,
        maxPriceMicros: priceFilter ? toMicros(priceFilter) : undefined,
      });
      if (epoch !== accountEpoch.current) return false;
      setOffers(result.offers);
      return Boolean(publishedKey && result.offers.some((offer) => offer.key.toLowerCase() === publishedKey.toLowerCase()));
    } catch (err) {
      if (epoch === accountEpoch.current) setOffersError(`Discovery unavailable. ${failure(err)}`);
      return false;
    } finally { if (epoch === accountEpoch.current) setOffersLoading(false); }
  }

  async function linkCustomerWallet() {
    await run("Linking customer wallet", async (isCurrent) => {
      const attached = browserWallet();
      if (!attached) throw new Error("Open this page in an EVM wallet browser or enable a browser wallet.");
      const address = await walletAccount(attached, true);
      await switchWalletChain(attached, ARC_CHAIN, config?.rpcUrl);
      const challenge = await purchaseAPI.challengeWallet(address);
      if (!isCurrent() || expired(challenge.expiresAt)) throw new Error("The wallet-link challenge expired. Try linking again.");
      await checkWallet(attached, address, ARC_CHAIN);
      const signature = await signWalletChallenge(attached, address, challenge.message);
      if (!isCurrent()) return;
      const linked = await purchaseAPI.linkWallet(challenge.id, signature);
      if (!isCurrent()) return;
      await refreshAllowance(true);
      if (!isCurrent()) return;
      setWallet(linked); setAccountError("");
      await inspectWallet();
    });
  }

  async function refreshAccount() {
    await run("Refreshing account", async (isCurrent) => {
      const [linked, settings] = await Promise.allSettled([purchaseAPI.wallet(), purchaseAPI.config()]);
      if (!isCurrent()) return;
      if (settings.status === "fulfilled") { setConfig(settings.value); setConfigError(""); }
      else { setConfig(null); setConfigError("Purchase services are unavailable. No payment actions can start."); }
      if (linked.status === "fulfilled") setWallet(linked.value);
      else setWallet(null);
      setAccountError(linked.status === "rejected" ? "The linked wallet could not be confirmed." : "");
      if (linked.status === "fulfilled" && linked.value) await refreshAllowance();
      else { allowanceRequest.current += 1; setAllowance(null); setAllowanceError(""); }
    });
  }

  async function saveLimit(event: FormEvent) {
    event.preventDefault();
    await run("Saving spending limit", async (isCurrent) => {
      try { await purchaseAPI.setAllowance(toMicros(limit)); }
      finally { if (isCurrent()) await refreshAllowance(); }
    });
  }

  async function createOrder(event: FormEvent) {
    event.preventDefault();
    if (!selected || offerUnavailable(selected) || !wallet || !config?.enabled) return;
    await run("Creating immutable order", async (isCurrent) => {
      const request: unknown = JSON.parse(requestText);
      if (!request || typeof request !== "object" || Array.isArray(request)) throw new Error("Service input must be a JSON object matching its schema.");
      const maxMicros = toMicros(maxCost);
      if (BigInt(maxMicros) < BigInt(selected.priceMicros)) throw new Error("The maximum cost is below this offer's price.");
      const terms = JSON.stringify([selected.key, request, maxMicros]);
      if (requestId.current?.terms !== terms) requestId.current = { terms, id: crypto.randomUUID() };
      const order = await purchaseAPI.createOrder({ offerKey: selected.key, request, maxMicros, clientRequestId: requestId.current.id });
      if (isCurrent()) { current.select(order); setSelected(null); requestId.current = null; }
    });
  }

  async function reviewedOrder(): Promise<PurchaseOrder> {
    const reviewed = current.order;
    if (!reviewed) throw new Error("Choose an order first.");
    const fresh = await current.refresh(reviewed.id);
    if (!fresh) throw new Error("The current order could not be confirmed. No action was taken.");
    if (immutableOrder(reviewed) !== immutableOrder(fresh)) throw new Error("Order terms changed. Review the stored terms again before continuing.");
    if (expired(fresh.deadline)) throw new Error("This order has expired. Reconcile any existing payment before creating another.");
    return fresh;
  }

  async function verifyIdentity() {
    await run("Opening fresh verification", async (isCurrent) => {
      const order = await reviewedOrder();
      const result = await purchaseAPI.beginIdentity(order.id);
      if (!isCurrent()) return;
      const url = new URL(result.authorizationUrl, window.location.origin);
      const official = url.protocol === "https:" && ["auth.world.org", "sandbox.auth.world.org"].includes(url.hostname);
      const simulation = config?.evidenceMode === "simulation" && ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname) && ["http:", "https:"].includes(url.protocol);
      if ((!official && !simulation) || url.username || url.password || expired(result.expiresAt)) throw new Error("The identity return flow is unavailable or expired.");
      window.location.assign(url.href);
    });
  }

  async function decide(decision: "approve" | "deny") {
    await run(decision === "approve" ? "Recording approval" : "Recording denial", async (isCurrent) => {
      const order = await reviewedOrder();
      if (!order.canApprove) throw new Error("Fresh verification is required before this decision.");
      current.invalidate();
      try {
        const next = await purchaseAPI.decideOrder(order.id, order.intentHash, decision);
        if (isCurrent()) current.accept(next);
      } finally { if (isCurrent()) await current.refresh(order.id); }
    });
  }

  async function authorize() {
    await run("Waiting for exact payment signature", async (isCurrent) => {
      const order = await reviewedOrder();
      const attached = browserWallet();
      if (!attached || !order.canAuthorize) throw new Error("This order is not ready for wallet authorization.");
      await checkWallet(attached, order.payer, ARC_CHAIN);
      current.invalidate();
      try {
        const prepared = await purchaseAPI.prepareAuthorization(order.id);
        if (!isCurrent()) return;
        current.accept(prepared.order);
        assertAuthorizationMatches(order, prepared, await walletAccount(attached));
        const signature = await signPurchase(attached, order.payer, ARC_CHAIN, prepared.typedData);
        if (!isCurrent()) return;
        const next = await purchaseAPI.submitAuthorization(order.id, signature);
        if (isCurrent()) current.accept(next);
      } finally { if (isCurrent()) await Promise.all([current.refresh(order.id), refreshAllowance()]); }
    });
  }

  async function reconcile() {
    if (!current.order) return;
    const id = current.order.id;
    await run("Reconciling original order", async (isCurrent) => {
      current.invalidate();
      try { const next = await purchaseAPI.reconcileOrder(id); if (isCurrent()) current.accept(next); }
      finally { if (isCurrent()) await Promise.all([current.refresh(id), refreshAllowance()]); }
    });
  }

  async function cancel() {
    await run("Cancelling unsigned order", async (isCurrent) => {
      const order = await reviewedOrder();
      if (!order.canCancel || order.signatureReleasedAt) throw new Error("Authorization may already be usable. Reconcile this order instead of cancelling.");
      current.invalidate();
      try { const next = await purchaseAPI.cancelOrder(order.id); if (isCurrent()) current.accept(next); }
      finally { if (isCurrent()) await Promise.all([current.refresh(order.id), refreshAllowance()]); }
    });
  }

  const unresolved = Boolean(current.order?.signatureReleasedAt) && !["completed", "denied", "cancelled", "expired"].includes(current.order?.state ?? "");
  const accountMatches = Boolean(wallet && account) && wallet?.address.toLowerCase() === account.toLowerCase();
  const ready = Boolean(config?.enabled && wallet && allowance && !accountError && !allowanceError && auth.signedIn);
  const arkivRPC = process.env.NEXT_PUBLIC_ARKIV_RPC_URL;
  const catalogNetworkMatches = config?.arkivChainId === ARKIV_CHAIN_ID;

  return <><Topbar /><main className={styles.page}>
    <header className={styles.hero}>
      <div><p className={styles.eyebrow}>AgentMesh / Service purchases</p><h1 className={styles.title}>Choose a service.<br />Approve the exact cost.</h1><p className={styles.lead}>Discover provider-owned offers, verify freshly and authorize one bounded USDC payment from your linked wallet.</p></div>
      <span className={styles.badge}>ARC TESTNET · CHAIN 5042002</span>
    </header>
    {auth.loading ? <p role="status">Checking your account...</p> : !auth.signedIn ? <section className={styles.card}>
      <h2>{auth.offline ? "Account connection unavailable" : "Sign in to purchase services"}</h2>
      <p className={styles.muted}>Orders, wallet links and spending limits belong to your authenticated account.</p>
      {auth.offline ? <button className={styles.button} onClick={auth.retry}>Retry account connection</button> : <Link className={styles.button} href="/signin">Sign in</Link>}
    </section> : <div className={styles.stack}>
      {config?.evidenceMode === "simulation" && <div className={styles.warning} role="status">Local simulation. Identity, payment and merchant results in this mode do not establish live identity assurance or onchain settlement.</div>}
      {config?.evidenceMode === "live" && <p className={styles.notice}>Live adapter configuration, Arc testnet. Each operation still needs a confirmed response. World sandbox or demo identity assurance is not production person assurance.</p>}
      {configError && <p className={styles.error} role="alert">{configError}</p>}
      {config && !config.enabled && <section className={styles.warning} aria-label="Purchase configuration unavailable"><strong>Checkout is unavailable.</strong><p>You can still inspect returned service offers. Provider publishing uses its own wallet and Arkiv connection.</p><ul>{config.blockedReasons.map((reason) => <li key={reason}>{reason}</li>)}</ul></section>}
      {error && <p className={styles.error} role="alert">{error}</p>}
      {busy && <p className={styles.notice} role="status">{busy}...</p>}
      <div className={styles.grid}>
        <div className={styles.stack}>
          <section className={styles.card} aria-label="Customer wallet and spending limit">
            <h2>Your purchase wallet</h2>
            <p className={styles.muted}>Connecting a wallet is separate from proving its ownership and from human verification. This flow supports an EOA wallet in a browser or mobile wallet browser.</p>
            <dl className={styles.facts}>
              <dt>Connected account</dt><dd className={styles.mono}>{account || "Disconnected"}</dd>
              <dt>Wallet network</dt><dd>{chain === null ? "Unavailable" : chain === ARC_CHAIN ? "Arc testnet" : `Chain ${chain}, switch to Arc for payment`}</dd>
              <dt>Account-linked wallet</dt><dd className={styles.mono}>{wallet?.address ?? "Not linked"}</dd>
            </dl>
            {!provider && <p className={styles.notice}>No browser wallet found. Open this page in a wallet browser or enable an EVM browser wallet.</p>}
            {walletError && <p className={styles.notice}>{walletError}</p>}
            {accountError && <p className={styles.error} role="alert">{accountError}</p>}
            <div className={styles.row}>
              <button className={styles.button} disabled={Boolean(busy) || !provider || !config} onClick={() => void linkCustomerWallet()}>{accountMatches && chain === ARC_CHAIN ? "Verify wallet ownership again" : "Connect and link on Arc"}</button>
              <button className={styles.secondary} disabled={Boolean(busy)} onClick={() => void refreshAccount()}>Refresh account</button>
            </div>
            {wallet && <form className={styles.step} onSubmit={(event) => void saveLimit(event)}>
              <label className={styles.field}>AgentMesh spending limit (USDC)<input className={styles.input} inputMode="decimal" value={limit} onChange={(event) => setLimit(event.target.value)} required /></label>
              <p className={styles.muted}>This limit applies to AgentMesh purchases. It does not constrain transactions made elsewhere from your wallet.</p>
              {allowance && <p className={styles.muted}>{usdc(allowance.availableMicros)} available · {usdc(allowance.reservedMicros)} reserved · {usdc(allowance.consumedMicros)} spent</p>}
              {!allowance && !allowanceError && <p className={styles.muted} role="status">Refreshing spending limit...</p>}
              {allowanceError && <p className={styles.error} role="alert">{allowanceError}</p>}
              <button className={styles.secondary} disabled={Boolean(busy) || !limit} type="submit">Set purchase limit</button>
            </form>}
          </section>

          <section className={styles.card} aria-label="Discover service offers">
            <h2>Live service offers</h2>
            <p className={styles.muted}>{config?.evidenceMode === "simulation" ? "Simulation catalogue for Arkiv Rabdi. It does not establish a live Arkiv read." : "Read from Arkiv Rabdi through the purchase service. Ownership and expiry are checked again before spending."}</p>
            <p className={styles.notice}>Tiramisu listings do not carry over to Rabdi. For an unsigned order from the old network, cancel it and create a new order from a current listing. Reconcile any already released payment using its original order.</p>
            <form onSubmit={(event) => { event.preventDefault(); void refreshOffers(); }}>
              <label className={styles.field}>Capability<input className={styles.input} value={capability} onChange={(event) => setCapability(event.target.value)} placeholder="All available capabilities" /></label>
              <label className={styles.field}>Maximum offer price (USDC)<input className={styles.input} inputMode="decimal" value={priceFilter} onChange={(event) => setPriceFilter(event.target.value)} placeholder="No price filter" /></label>
              <button className={styles.secondary} type="submit" disabled={offersLoading || Boolean(busy)}>Refresh discovery</button>
            </form>
            {offersError && <p className={styles.error} role="alert">{offersError}</p>}
            {offersLoading ? <p className={styles.empty} role="status">Reading current service offers...</p> : offers.length === 0 ? <p className={styles.empty}>{offersError ? "No current listing could be confirmed." : "No matching offers were returned. Refresh or publish a provider offer."}</p> : <ul className={styles.list} style={{ marginTop: 18 }}>
              {offers.map((offer) => {
                const unavailable = offerUnavailable(offer, now);
                return <li className={styles.offer} key={offer.key}>
                  <div className={styles.offerHeader}><h3>{offer.capability}</h3><span className={styles.price}>{usdc(offer.priceMicros)} USDC</span></div>
                  <p className={styles.mono} style={{ margin: 0 }}>Owner / payee: {offer.owner}</p>
                  <p className={styles.muted} style={{ margin: 0 }}>Version {offer.version} · Valid until {offer.validUntil}<br />Arkiv block {offer.observedBlock} observed; expires at {offer.expiresAtBlock}.</p>
                  {unavailable && <p className={styles.warning}>{unavailable}</p>}
                  <button className={styles.secondary} disabled={Boolean(busy) || Boolean(offersError)} onClick={() => { setSelected(offer); setMaxCost(usdc(offer.priceMicros)); setRequestText("{}"); }}>Inspect service and price</button>
                </li>;
              })}
            </ul>}
          </section>
          {config?.arkivAvailable && !catalogNetworkMatches && <p className={styles.error} role="alert">Provider publishing is unavailable because the browser and catalog networks differ.</p>}
          <ProviderOfferForm provider={provider} walletRevision={walletRevision} rpcURL={arkivRPC} disabled={Boolean(busy) || !config?.arkivAvailable || !catalogNetworkMatches} simulation={config?.evidenceMode === "simulation"} onWalletChange={() => void inspectWallet()} onPublished={refreshOffers} />
        </div>
        <div className={styles.stack}>
          {current.error && <div className={styles.error} role="alert">{current.error}<div className={styles.row}><button className={styles.secondary} disabled={Boolean(busy)} onClick={() => void current.refresh()}>Retry order refresh</button></div></div>}
          {selected && <section className={styles.card} aria-label="Review service request">
            <h2>{selected.capability}</h2>
            <dl className={styles.facts}>
              <dt>Price</dt><dd>{usdc(selected.priceMicros)} USDC</dd><dt>Payee</dt><dd className={styles.mono}>{selected.payee}</dd>
              <dt>Service</dt><dd className={styles.mono}>{selected.method} {selected.endpoint}</dd><dt>Token / chain</dt><dd className={styles.mono}>{selected.asset} / {selected.chainId}</dd>
              <dt>Offer deadline</dt><dd>{selected.validUntil}</dd><dt>Offer key</dt><dd className={styles.mono}>{selected.key}</dd>
            </dl>
            <details className={styles.details}><summary>Required service input schema</summary><pre className={styles.result}>{JSON.stringify(selected.inputSchema, null, 2)}</pre></details>
            <form onSubmit={(event) => void createOrder(event)}>
              <label className={styles.field}>Service input (private JSON)<textarea className={styles.textarea} value={requestText} onChange={(event) => setRequestText(event.target.value)} required spellCheck={false} /></label>
              <label className={styles.field}>Maximum purchase cost (USDC)<input className={styles.input} inputMode="decimal" value={maxCost} onChange={(event) => setMaxCost(event.target.value)} required /></label>
              <p className={styles.muted}>Creating an order retrieves the merchant&apos;s exact challenge. It does not approve, sign or pay. Your inputs are not published on Arkiv.</p>
              <button className={styles.button} disabled={Boolean(busy) || !ready || unresolved || Boolean(offersError) || Boolean(offerUnavailable(selected, now))}>Create exact order</button>
            </form>
          </section>}
          {current.order ? <OrderPanel order={current.order} config={config} account={account} busy={Boolean(busy)} stale={current.stale} now={now} onIdentity={() => void verifyIdentity()} onDecision={(decision) => void decide(decision)} onSign={() => void authorize()} onReconcile={() => void reconcile()} onCancel={() => void cancel()} onRefresh={() => void current.refresh()} /> : !selected && <section className={styles.card}>
            <h2>Your exact purchase</h2><p className={styles.muted}>{current.orderId ? "Restoring the order from the server..." : "Link your wallet, choose a service and inspect its exact terms. Verification and approval happen before any payment signature."}</p>
            <ol className={styles.muted}><li>Inspect the provider-owned offer.</li><li>Verify freshly, then approve the stored order.</li><li>Sign the exact payment and retrieve its result.</li></ol>
          </section>}
        </div>
      </div>
    </div>}
  </main></>;
}
