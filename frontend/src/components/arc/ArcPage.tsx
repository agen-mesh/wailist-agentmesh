"use client";

import { useEffect, useState } from "react";
import { Topbar } from "@/components/Topbar";
import { apiFetch, BASE } from "@/lib/api";

type ChainView = {
  id: string;
  name: string;
  chainId: number;
  caip2: string;
  cctp: boolean;
  gateway: boolean;
  family: string;
};

type ChainsPayload = {
  network: string;
  walletConfigured: boolean;
  address: string;
  payTo: string;
  studioURL: string;
  maxUsdc: string;
  escrow: string;
  keyError: string;
  chains: ChainView[];
};

type Balance = {
  chainId: string;
  chain: string;
  symbol: string;
  amount: string;
  error?: string;
  includedInUsdc: boolean;
};

type Treasury = {
  walletUsdc: string;
  gatewayUsdc: string;
  partial: boolean;
  gatewayError?: string;
  balances: Balance[];
};

const FALLBACK: ChainView[] = [
  { id: "arc-testnet", name: "Arc Testnet", chainId: 5042002, caip2: "eip155:5042002", cctp: true, gateway: true, family: "evm" },
  { id: "ethereum-sepolia", name: "Ethereum Sepolia", chainId: 11155111, caip2: "eip155:11155111", cctp: true, gateway: true, family: "evm" },
  { id: "base-sepolia", name: "Base Sepolia", chainId: 84532, caip2: "eip155:84532", cctp: true, gateway: true, family: "evm" },
  { id: "algorand-testnet", name: "Algorand Testnet", chainId: 0, caip2: "algorand:testnet", cctp: false, gateway: false, family: "algorand" },
];

const CSS = `
.arc-page { max-width: 1080px; margin: 0 auto; padding: 36px 24px 96px; }
.arc-kicker { font-family: var(--font-mono); font-size: var(--t-1); letter-spacing: 0.14em; text-transform: uppercase; color: #FFB547; margin: 0 0 8px; }
.arc-title { margin: 0; font-size: clamp(32px, 5vw, 52px); letter-spacing: -0.04em; line-height: 0.95; font-weight: 650; }
.arc-lead { margin: 14px 0 0; max-width: 62ch; color: var(--fg-muted); line-height: 1.5; font-size: var(--t-4); }
.arc-grid { display: grid; grid-template-columns: minmax(0, 1.35fr) minmax(280px, 0.85fr); gap: 18px; margin-top: 28px; }
.arc-card { background: var(--bg-elev-1, #12111a); border: 1px solid var(--border); border-radius: 16px; padding: 18px; }
.arc-card h2 { margin: 0 0 12px; font-size: var(--t-4); letter-spacing: -0.02em; }
.arc-row { display: flex; gap: 8px; flex-wrap: wrap; }
.arc-row input, .arc-row select, .arc-field input, .arc-field select {
  height: 38px; border-radius: 10px; border: 1px solid var(--border); background: var(--bg); color: var(--fg);
  padding: 0 10px; font-family: var(--font-mono); font-size: var(--t-1); min-width: 0;
}
.arc-field { display: flex; flex-direction: column; gap: 6px; margin-bottom: 10px; }
.arc-field span { font-size: var(--t-1); color: var(--fg-muted); }
.arc-btn { height: 38px; border-radius: 10px; border: 1px solid #FFB547; background: #FFB547; color: #1a1204; font-weight: 650; padding: 0 14px; cursor: pointer; }
.arc-btn.ghost { background: transparent; color: #FFB547; }
.arc-btn:disabled { opacity: 0.55; cursor: wait; }
.arc-figures { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin: 12px 0; }
.arc-fig { padding: 12px; border-radius: 12px; background: var(--bg); border: 1px solid var(--border); }
.arc-fig b { display: block; font-family: var(--font-mono); font-size: 22px; letter-spacing: -0.03em; }
.arc-fig em { font-style: normal; color: var(--fg-muted); font-size: var(--t-1); }
.arc-fig.gateway b { color: #E879F9; }
.arc-fig.wallet b { color: #FFB547; }
.arc-error { color: #ffb4b4; font-size: var(--t-2); line-height: 1.45; margin: 8px 0 0; }
.arc-out { margin: 10px 0 0; white-space: pre-wrap; word-break: break-word; font-family: var(--font-mono); font-size: 11px; line-height: 1.45; color: var(--fg-muted); max-height: 220px; overflow: auto; }
.arc-links { display: flex; gap: 14px; flex-wrap: wrap; margin-top: 14px; font-size: var(--t-2); }
.arc-links a { color: #FFB547; }
.arc-balances { display: flex; flex-direction: column; gap: 6px; margin-top: 8px; }
.arc-bal { display: flex; justify-content: space-between; gap: 12px; font-size: var(--t-1); font-family: var(--font-mono); }
@media (max-width: 900px) {
  .arc-grid { grid-template-columns: 1fr; }
  .arc-page { padding: 24px 16px 88px; }
}
@media (max-width: 420px) {
  .arc-figures { grid-template-columns: 1fr; }
}
`;

function Diagram() {
  const nodes = [
    { id: "canvas", label: "Canvas", x: 16 },
    { id: "node", label: "Arc node", x: 150 },
    { id: "api", label: "Go API", x: 284 },
    { id: "pay", label: "EIP-3009", x: 418 },
  ];
  return (
    <svg viewBox="0 0 640 220" role="img" aria-label="AgentMesh Arc architecture" style={{ width: "100%", height: "auto" }}>
      <rect width="640" height="220" rx="14" fill="#100e18" />
      {nodes.map((n, i) => (
        <g key={n.id}>
          {i < nodes.length - 1 && <line x1={n.x + 96} y1="46" x2={nodes[i + 1].x} y2="46" stroke="#3a3450" />}
          <rect x={n.x} y="24" width="108" height="44" rx="10" fill="#1b1830" stroke="#FFB547" />
          <text x={n.x + 54} y="50" textAnchor="middle" fill="#f4efe4" fontSize="12" fontFamily="ui-monospace, monospace">{n.label}</text>
        </g>
      ))}
      <line x1="338" y1="68" x2="180" y2="120" stroke="#E879F9" />
      <line x1="338" y1="68" x2="338" y2="120" stroke="#A78BFA" />
      <line x1="338" y1="68" x2="500" y2="120" stroke="#8B5CF6" />
      {[
        { t: "CCTP burn and mint", d: "Base, Ethereum, and the rest", x: 24 },
        { t: "Gateway balance", d: "Counted on its own", x: 230 },
        { t: "Algorand x402", d: "Left on its own path", x: 436 },
      ].map((box) => (
        <g key={box.t}>
          <rect x={box.x} y="118" width="180" height="68" rx="10" fill="#161322" stroke="#2e2942" />
          <text x={box.x + 90} y="146" textAnchor="middle" fill="#f4efe4" fontSize="12" fontFamily="ui-sans-serif, system-ui">{box.t}</text>
          <text x={box.x + 90} y="166" textAnchor="middle" fill="#9b95a8" fontSize="11" fontFamily="ui-sans-serif, system-ui">{box.d}</text>
        </g>
      ))}
    </svg>
  );
}

export function ArcPage() {
  const [chains, setChains] = useState<ChainsPayload | null>(null);
  const [loadError, setLoadError] = useState(BASE ? "" : "Set NEXT_PUBLIC_API_URL so this page can reach the treasury API. The diagram still describes the flow.");
  const [address, setAddress] = useState("");
  const [algo, setAlgo] = useState("");
  const [treasury, setTreasury] = useState<Treasury | null>(null);
  const [busy, setBusy] = useState("");
  const [op, setOp] = useState("nanopay");
  const [chain, setChain] = useState("arc-testnet");
  const [dest, setDest] = useState("base-sepolia");
  const [to, setTo] = useState("");
  const [amount, setAmount] = useState("0.01");
  const [output, setOutput] = useState("");

  useEffect(() => {
    if (!BASE) return;
    let live = true;
    apiFetch(`${BASE}/arc/chains`)
      .then(async (res) => {
        const body = await res.json().catch(() => null);
        if (!res.ok) throw new Error(body?.error || `chains ${res.status}`);
        if (live) {
          setChains(body);
          if (body.address) setAddress(body.address);
          if (body.payTo) setTo(body.payTo);
        }
      })
      .catch((err: Error) => {
        if (live) setLoadError(err.message || "The treasury API did not answer.");
      });
    return () => {
      live = false;
    };
  }, []);

  const list = chains?.chains?.length ? chains.chains : FALLBACK;
  const evm = list.filter((c) => c.family === "evm");

  async function run(path: string, init?: RequestInit) {
    if (!BASE) {
      setOutput("The API URL is not configured.");
      return;
    }
    setBusy(path);
    setOutput("");
    try {
      const res = await apiFetch(`${BASE}${path}`, init);
      const text = await res.text();
      let pretty = text;
      try {
        pretty = JSON.stringify(JSON.parse(text), null, 2);
      } catch {
        pretty = text;
      }
      if (res.status === 401) {
        setOutput("Sign in to broadcast. A quote does not move USDC.\n\n" + pretty);
      } else {
        setOutput(`${res.status}\n${pretty}`);
      }
      return { res, pretty };
    } catch (err) {
      setOutput(err instanceof Error ? err.message : "Request failed");
    } finally {
      setBusy("");
    }
  }

  async function lookup() {
    const q = new URLSearchParams();
    if (address.trim()) q.set("address", address.trim());
    if (algo.trim()) q.set("algorand", algo.trim());
    const result = await run(`/arc/treasury?${q.toString()}`);
    if (!result) return;
    try {
      const parsed = JSON.parse(result.pretty);
      if (parsed.walletUsdc !== undefined) setTreasury(parsed);
    } catch {
      setTreasury(null);
    }
  }

  function post(path: string, broadcast: boolean) {
    return run(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        op,
        chain,
        destChain: dest,
        to,
        amount,
        broadcast,
      }),
    });
  }

  const studio = chains?.studioURL || "";

  return (
    <div className="am-viewport" style={{ minHeight: "100dvh", display: "flex", flexDirection: "column", background: "var(--bg)", color: "var(--fg)" }}>
      <style>{CSS}</style>
      <Topbar />
      <div style={{ flex: 1, overflow: "auto" }}>
        <div className="arc-page">
          <p className="arc-kicker">Circle Arc · multichain USDC</p>
          <h1 className="arc-title">Nanopay the agents you already run.</h1>
          <p className="arc-lead">
            AgentMesh already monetizes tools with x402. This treasury adds
            EIP-3009 nanopayments on Arc, CCTP burn-and-mint to the other
            Circle chains, and a Gateway balance that is counted on its own.
            Bounty: nanopayment infrastructure shipped into an existing AI
            agent and API monetization tool.
          </p>
          <div className="arc-links">
            <a href="https://studio.arc.com" target="_blank" rel="noreferrer">Arc Studio</a>
            {studio ? <a href={studio} target="_blank" rel="noreferrer">This Studio project</a> : <span>Set ARC_STUDIO_URL after you create the Studio project.</span>}
            <a href="https://faucet.circle.com" target="_blank" rel="noreferrer">Testnet faucet</a>
            {chains?.network ? <span>Server mode {chains.network}</span> : null}
          </div>
          {loadError ? <p className="arc-error">{loadError}</p> : null}

          <div className="arc-grid">
            <section className="arc-card">
              <h2>How a USDC step moves</h2>
              <Diagram />
              <p style={{ margin: "12px 0 0", color: "var(--fg-muted)", fontSize: "var(--t-2)", lineHeight: 1.5 }}>
                Wallet USDC is the ERC-20 balance, 6 decimals. On Arc the native
                gas balance is the same asset at 18 decimals and is not added in.
                EURC is shown beside it. Algorand stays on its existing x402 path
                and is not a CCTP source.
              </p>
            </section>

            <section className="arc-card">
              <h2>Balances</h2>
              <div className="arc-field">
                <span>EVM address</span>
                <input value={address} onChange={(e) => setAddress(e.target.value)} placeholder="0x…" />
              </div>
              <div className="arc-field">
                <span>Algorand address</span>
                <input value={algo} onChange={(e) => setAlgo(e.target.value)} placeholder="58-character address" />
              </div>
              <button className="arc-btn" disabled={!!busy} onClick={() => void lookup()}>
                {busy.includes("treasury") ? "Reading…" : "Look up"}
              </button>
              <div className="arc-figures">
                <div className="arc-fig wallet">
                  <em>Wallet USDC</em>
                  <b>{treasury?.walletUsdc ?? "—"}</b>
                </div>
                <div className="arc-fig gateway">
                  <em>Gateway USDC</em>
                  <b>{treasury?.gatewayUsdc ?? "—"}</b>
                </div>
              </div>
              {treasury?.partial ? <p className="arc-error">Some chains did not answer. The sum includes only the reads that succeeded.</p> : null}
              {treasury?.gatewayError ? <p className="arc-error">Gateway: {treasury.gatewayError}</p> : null}
              <div className="arc-balances">
                {(treasury?.balances ?? []).filter((b) => b.symbol === "EURC" || b.error).map((b) => (
                  <div className="arc-bal" key={b.chainId + b.symbol}>
                    <span>{b.chain} {b.symbol}</span>
                    <span>{b.error || b.amount}</span>
                  </div>
                ))}
              </div>
              {chains?.keyError ? <p className="arc-error">{chains.keyError}</p> : null}
              <p style={{ margin: "10px 0 0", color: "var(--fg-dim)", fontSize: "var(--t-1)" }}>
                {chains?.walletConfigured ? `Spending address ${chains.address}. Cap ${chains.maxUsdc} USDC.` : "No spending key on this server. Quotes and balance reads still work."}
                {chains?.escrow ? ` Escrow ${chains.escrow}.` : ""}
              </p>
            </section>
          </div>

          <div className="arc-grid">
            <section className="arc-card">
              <h2>Quote, then send</h2>
              <div className="arc-field">
                <span>Operation</span>
                <select value={op} onChange={(e) => setOp(e.target.value)}>
                  <option value="nanopay">Nanopay (EIP-3009)</option>
                  <option value="transfer">Transfer</option>
                  <option value="bridge">CCTP bridge</option>
                  <option value="gateway">Gateway deposit</option>
                </select>
              </div>
              <div className="arc-row">
                <div className="arc-field" style={{ flex: 1 }}>
                  <span>Chain</span>
                  <select value={chain} onChange={(e) => setChain(e.target.value)}>
                    {evm.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
                  </select>
                </div>
                <div className="arc-field" style={{ flex: 1 }}>
                  <span>Destination</span>
                  <select value={dest} onChange={(e) => setDest(e.target.value)}>
                    {evm.filter((c) => c.cctp).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
                  </select>
                </div>
              </div>
              <div className="arc-field">
                <span>Recipient</span>
                <input value={to} onChange={(e) => setTo(e.target.value)} placeholder="0x…" />
              </div>
              <div className="arc-field">
                <span>Amount (USDC)</span>
                <input value={amount} onChange={(e) => setAmount(e.target.value)} />
              </div>
              <div className="arc-row">
                <button className="arc-btn ghost" disabled={!!busy} onClick={() => void post("/arc/quote", false)}>Quote</button>
                <button className="arc-btn" disabled={!!busy} onClick={() => void post(op === "gateway" ? "/arc/gateway/deposit" : op === "bridge" ? "/arc/bridge" : op === "transfer" ? "/arc/transfer" : "/arc/nanopay", true)}>
                  Broadcast
                </button>
              </div>
            </section>

            <section className="arc-card">
              <h2>402 challenge</h2>
              <p style={{ margin: "0 0 12px", color: "var(--fg-muted)", fontSize: "var(--t-2)", lineHeight: 1.5 }}>
                GET /x402/arc answers 402 with an exact USDC requirement on Arc.
                The one-click buy is signed in. Without a spending key the
                challenge is refused rather than naming the zero address.
              </p>
              <div className="arc-row">
                <button className="arc-btn ghost" disabled={!!busy} onClick={() => void run(`/x402/arc?amount=${encodeURIComponent(amount || "0.01")}`)}>
                  Probe challenge
                </button>
                <button className="arc-btn" disabled={!!busy} onClick={() => void run("/x402/arc/buy", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ amount, to }) })}>
                  One-click buy
                </button>
              </div>
              <pre className="arc-out">{output || "Quote, balance, and challenge output lands here."}</pre>
            </section>
          </div>
        </div>
      </div>
    </div>
  );
}
