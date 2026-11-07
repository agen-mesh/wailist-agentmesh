/**
 * The chain AgentMesh settles x402 payments on, for UI copy and explorer links.
 *
 * Mirrors backend/internal/chain: one process settles on one rail, chosen there
 * by X402_CHAIN. Set NEXT_PUBLIC_X402_CHAIN to the same value so what the UI
 * says matches what the backend actually pays with. Blank and "whitechain"
 * are Whitechain, the backend's default. Any other value throws: a typo must
 * not label an Arc USDC settlement as USDT.
 *
 * Receipts returned by the API carry their own explorerURL — prefer that over
 * building one here. EXPLORER is for fixtures and for the rare place the API
 * gives a bare hash.
 */
type ChainInfo = {
  /** Human-readable chain name for copy, e.g. "settled on <name>". */
  name: string;
  /** The settlement token's symbol, shown next to amounts. */
  token: string;
  /** CAIP-2 network id, as it appears in an x402 challenge. */
  network: string;
  /** Explorer origin, no trailing slash. */
  explorer: string;
  /**
   * tinyx402's paid web-search endpoint on this chain, used by the demo
   * workflow. These endpoints are per chain — each quotes only its own
   * asset, and the old chain-less /api/v1/search now answers
   * {"error":"endpoint_moved"}.
   */
  demoSearchEndpoint: string;
};

const WHITECHAIN: ChainInfo = {
  name: "Whitechain testnet",
  token: "USDT",
  network: "eip155:1874",
  explorer: "https://explorer.testnet.whitechain.io",
  demoSearchEndpoint: "https://tinyx402.007575.xyz/api/whitechain/v1/search",
};

/** Arc Testnet: USDC is both the settlement asset and the gas token. */
const ARC: ChainInfo = {
  name: "Arc Testnet",
  token: "USDC",
  network: "eip155:5042002",
  explorer: "https://explorer.testnet.arc.io",
  demoSearchEndpoint: "https://tinyx402.007575.xyz/api/arc/v1/search",
};

/** Resolves NEXT_PUBLIC_X402_CHAIN. Exported so a typo can be tested without rebuilding. */
export function chainForEnv(raw: string | undefined): ChainInfo {
  const id = raw?.trim().toLowerCase() ?? "";
  if (id === "" || id === "whitechain") return WHITECHAIN;
  if (id === "arc") return ARC;
  throw new Error(
    `NEXT_PUBLIC_X402_CHAIN=${JSON.stringify(raw)} is not a known rail (want whitechain or arc)`,
  );
}

export const CHAIN: ChainInfo = chainForEnv(process.env.NEXT_PUBLIC_X402_CHAIN);

/** Explorer link for a transaction hash. Empty in, empty out. */
export function txURL(hash: string): string {
  return hash ? `${CHAIN.explorer}/tx/${hash}` : "";
}
