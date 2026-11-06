import { createPublicClient, createWalletClient, ExpirationTime } from "@arkiv-network/sdk";
import { addr, bytes, defineSchema, str, u64 } from "@arkiv-network/sdk/schema";
import { rabdi } from "@arkiv-network/sdk/chains";
import { custom, http } from "viem";
import {
  ARC_ASSET, ARC_CHAIN_ID, ARKIV_CHAIN_ID, OFFER_KIND, OFFER_PROJECT, OFFER_SCHEMA,
  OfferWriteError, isAddress, isHash, isHTTPSURL, prepareOffer,
  type OfferDraft,
} from "./schema";

const offerSchema = defineSchema(OFFER_SCHEMA, {
  project: str().indexed(),
  kind: str().indexed(),
  capability: str().indexed(),
  settlement_chain: u64().indexed(),
  asset: addr(),
  price_micros: u64().indexed(),
  version: u64(),
  status: str().indexed(),
  valid_until: u64(),
  content_type: str(),
  payload: bytes(),
});

export type WalletProvider = {
  request(args: { method: string; params?: readonly unknown[] | object }): Promise<unknown>;
};

export type WalletState =
  | { status: "unconfigured" | "disconnected" | "wrong-network" | "error"; message: string }
  | { status: "ready"; account: `0x${string}`; chainId: typeof ARKIV_CHAIN_ID; currentBlock: string };

export type PublishedOffer = {
  entityKey: `0x${string}`;
  txHash: `0x${string}`;
  expiresAtBlock: string;
  owner: `0x${string}`;
};

function rpcQuantity(value: unknown): bigint {
  if (typeof value !== "string" || !/^0x(?:0|[1-9a-fA-F][0-9a-fA-F]{0,15})$/.test(value)) {
    throw new OfferWriteError("wallet-error", "The wallet returned an invalid network value.");
  }
  return BigInt(value);
}

function configured(provider: WalletProvider | undefined, rpcURL: string | undefined) {
  if (!rpcURL || !isHTTPSURL(rpcURL)) {
    throw new OfferWriteError("unconfigured", "Arkiv publishing is not configured.");
  }
  if (!provider) {
    throw new OfferWriteError("disconnected", "Connect a provider wallet to publish an offer.");
  }
  return { provider, rpcURL };
}

async function walletAccount(provider: WalletProvider): Promise<`0x${string}`> {
  const accounts = await provider.request({ method: "eth_accounts" });
  if (!Array.isArray(accounts) || !isAddress(accounts[0])) {
    throw new OfferWriteError("disconnected", "Connect a provider wallet to publish an offer.");
  }
  return accounts[0].toLowerCase() as `0x${string}`;
}

async function readyWallet(provider: WalletProvider, expectedAccount?: string) {
  const chain = rpcQuantity(await provider.request({ method: "eth_chainId" }));
  if (chain !== BigInt(ARKIV_CHAIN_ID)) {
    throw new OfferWriteError("wrong-network", "Switch the provider wallet to Arkiv Rabdi.");
  }
  const account = await walletAccount(provider);
  if (expectedAccount && account !== expectedAccount.toLowerCase()) {
    throw new OfferWriteError("account-changed", "The provider account changed. Review the offer again.");
  }
  return account;
}

export async function inspectProviderWallet(provider?: WalletProvider, rpcURL?: string): Promise<WalletState> {
  try {
    const config = configured(provider, rpcURL);
    const account = await readyWallet(config.provider);
    const block = rpcQuantity(await config.provider.request({ method: "eth_blockNumber" }));
    return { status: "ready", account, chainId: ARKIV_CHAIN_ID, currentBlock: block.toString() };
  } catch (error) {
    if (error instanceof OfferWriteError && ["unconfigured", "disconnected", "wrong-network"].includes(error.code)) {
      return { status: error.code as "unconfigured" | "disconnected" | "wrong-network", message: error.message };
    }
    return { status: "error", message: "The provider wallet could not be read. Reconnect and try again." };
  }
}

export async function connectProviderWallet(provider?: WalletProvider, rpcURL?: string): Promise<Extract<WalletState, { status: "ready" }>> {
  const config = configured(provider, rpcURL);
  try {
    await config.provider.request({ method: "eth_requestAccounts" });
    const chainId = `0x${ARKIV_CHAIN_ID.toString(16)}`;
    if (rpcQuantity(await config.provider.request({ method: "eth_chainId" })) !== BigInt(ARKIV_CHAIN_ID)) {
      try {
        await config.provider.request({ method: "wallet_switchEthereumChain", params: [{ chainId }] });
      } catch (error) {
        if (typeof error !== "object" || error === null || !("code" in error) || error.code !== 4902) throw error;
        const chain = rabdi({ accessKey: "" });
        await config.provider.request({ method: "wallet_addEthereumChain", params: [{
          chainId, chainName: chain.name, nativeCurrency: chain.nativeCurrency,
          rpcUrls: [config.rpcURL],
        }] });
        await config.provider.request({ method: "wallet_switchEthereumChain", params: [{ chainId }] });
      }
    }
    const state = await inspectProviderWallet(config.provider, config.rpcURL);
    if (state.status !== "ready") throw new OfferWriteError("wallet-error", state.message);
    return state;
  } catch (error) {
    if (error instanceof OfferWriteError) throw error;
    throw new OfferWriteError("wallet-error", "Wallet connection was not completed.");
  }
}

export async function publishOffer(options: {
  provider?: WalletProvider;
  rpcURL?: string;
  expectedAccount: string;
  offer: OfferDraft;
  validateEndpoint: (endpoint: string) => Promise<void>;
}): Promise<PublishedOffer> {
  const { provider, rpcURL } = configured(options.provider, options.rpcURL);
  const expectedAccount = options.expectedAccount;
  const prepared = prepareOffer(options.offer, expectedAccount, Math.floor(Date.now() / 1000));
  if (typeof options.validateEndpoint !== "function") {
    throw new OfferWriteError("unconfigured", "Endpoint validation is not configured.");
  }
  try {
    await options.validateEndpoint(prepared.payload.endpoint);
  } catch {
    throw new OfferWriteError("endpoint-rejected", "The service endpoint is not permitted.");
  }
  let submittedHash: `0x${string}` | undefined;
  let sendAttempted = false;
  const chain = { ...rabdi({ accessKey: "" }), rpcUrls: { default: { http: [rpcURL] } } };
  const reader = createPublicClient({ chain, transport: http(rpcURL, { retryCount: 0 }) });
  const checkTerms = async () => {
    const account = await readyWallet(provider, expectedAccount);
    if (await reader.getChainId() !== ARKIV_CHAIN_ID) {
      throw new OfferWriteError("wrong-network", "The configured offer reader is not on Arkiv Rabdi.");
    }
    const block = rpcQuantity(await provider.request({ method: "eth_blockNumber" }));
    if (block >= prepared.expiresAtBlock || BigInt(Math.floor(Date.now() / 1000)) >= prepared.validUntil) {
      throw new OfferWriteError("invalid-offer", "The offer expired before it could be published.");
    }
    return account;
  };
  try {
    const account = await checkTerms();
    const guardedProvider: WalletProvider = {
      async request(args) {
        if (args.method === "eth_sendTransaction" || args.method === "wallet_sendTransaction") {
          if (sendAttempted) {
            throw new OfferWriteError("write-uncertain", "A write was already attempted. Inspect the wallet activity before retrying.", submittedHash);
          }
          await checkTerms();
          const tx = Array.isArray(args.params) ? args.params[0] as Record<string, unknown> | undefined : undefined;
          if (!tx || typeof tx.from !== "string" || tx.from.toLowerCase() !== account || (tx.chainId !== undefined && rpcQuantity(tx.chainId) !== BigInt(ARKIV_CHAIN_ID))) {
            throw new OfferWriteError("account-changed", "The transaction no longer matches the provider wallet.");
          }
          sendAttempted = true;
          const hash = await provider.request(args);
          if (isHash(hash)) submittedHash = hash;
          return hash;
        }
        return provider.request(args);
      },
    };
    const client = createWalletClient({
      account,
      chain,
      transport: custom(guardedProvider, { retryCount: 0 }),
    });
    const values = {
      project: str(OFFER_PROJECT),
      kind: str(OFFER_KIND),
      capability: str(prepared.capability),
      settlement_chain: u64(BigInt(ARC_CHAIN_ID)),
      asset: addr(ARC_ASSET),
      price_micros: u64(prepared.priceMicros),
      version: u64(prepared.version),
      status: str("active"),
      valid_until: u64(prepared.validUntil),
      content_type: str("application/json"),
      payload: bytes(new TextEncoder().encode(JSON.stringify(prepared.payload))),
    };
    const result = await client.createEntity(offerSchema, values, {
      expiration: ExpirationTime.atBlock(prepared.expiresAtBlock),
    });
    if (!sendAttempted || !submittedHash || !isHash(result.txHash) || !isHash(result.entityKey) || result.expiresAt !== prepared.expiresAtBlock || result.txHash.toLowerCase() !== submittedHash.toLowerCase()) {
      throw new OfferWriteError("write-uncertain", "The write result could not be verified. Inspect the transaction before retrying.", submittedHash);
    }
    const entity = await reader.getEntity(result.entityKey);
    await checkTerms();
    const head = await reader.getBlockNumber({ cacheTime: 0 });
    const expected = { ...values, "arkiv.schema": str(OFFER_SCHEMA) };
    const attributesMatch = Object.keys(entity.$attributes).length === Object.keys(expected).length &&
      Object.entries(expected).every(([name, wanted]) => {
        const actual = entity.$attributes[name];
        if (!actual || actual.type !== wanted.type) return false;
        return wanted.type === "addr" || wanted.type === "bytes"
          ? String(actual.value).toLowerCase() === wanted.value.toLowerCase()
          : actual.value === wanted.value;
      });
    if (entity.$key.toLowerCase() !== result.entityKey.toLowerCase() || entity.$owner.toLowerCase() !== account || entity.$expiresAt !== prepared.expiresAtBlock || entity.$expiresAt <= head || entity.$updatedAt > head || entity.$updatedAt >= entity.$expiresAt || !attributesMatch) {
      throw new OfferWriteError("write-uncertain", "The confirmed entity does not match the offer. Inspect the transaction before retrying.", submittedHash);
    }
    return { entityKey: result.entityKey, txHash: result.txHash, expiresAtBlock: result.expiresAt.toString(), owner: account };
  } catch (error) {
    const hash = submittedHash ?? (typeof error === "object" && error !== null && "txHash" in error && isHash(error.txHash) ? error.txHash : undefined);
    if (sendAttempted || hash) {
      if (error instanceof OfferWriteError && error.code === "write-uncertain") throw error;
      throw new OfferWriteError("write-uncertain", "The write outcome is unknown. Inspect the wallet activity before retrying.", hash);
    }
    if (error instanceof OfferWriteError) throw error;
    throw new OfferWriteError("write-rejected", "The offer was not confirmed. Check the wallet and network before retrying.");
  }
}
