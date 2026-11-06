// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { decodeAbiParameters, decodeFunctionData, hexToString, parseAbi, parseAbiParameters, stringToHex, type Transport } from "viem";
import { rabdi } from "@arkiv-network/sdk/chains";
import { connectProviderWallet, inspectProviderWallet, publishOffer } from "./wallet";
import { ARKIV_RPC_URL, prepareOffer, type OfferDraft } from "./schema";

const sdk = vi.hoisted(() => ({
  createClient: vi.fn(), createEntity: vi.fn(), createReader: vi.fn(),
  getChainId: vi.fn(), getEntity: vi.fn(), getBlockNumber: vi.fn(),
}));
vi.mock("@arkiv-network/sdk", async (importOriginal) => ({
  ...await importOriginal<typeof import("@arkiv-network/sdk")>(),
  createWalletClient: sdk.createClient,
  createPublicClient: sdk.createReader,
}));

const owner = "0x2222222222222222222222222222222222222222";
const txHash = `0x${"a".repeat(64)}` as const;
const entityKey = `0x${"1".repeat(64)}` as const;
const receiptResult = { entityKey, txHash, expiresAt: 4608n };
const payloadFixture = '{"endpoint":"https://merchant.example/tool","method":"POST","payee":"0x2222222222222222222222222222222222222222","input_schema":{"type":"object","properties":{"prompt":{"type":"string"}},"required":["prompt"],"additionalProperties":false}}';

function readbackFixture() {
  return {
    $key: entityKey, $owner: owner, $expiresAt: 4608n, $updatedAt: 4080n,
    $attributes: {
      "arkiv.schema": { type: "str", value: "agentmesh.service-offer.v2" },
      project: { type: "str", value: "agentmesh-mumbai" },
      kind: { type: "str", value: "service-offer-v2" },
      capability: { type: "str", value: "summarize" },
      settlement_chain: { type: "u64", value: 5042002n },
      asset: { type: "addr", value: "0x3600000000000000000000000000000000000000" },
      price_micros: { type: "u64", value: 1000000n },
      version: { type: "u64", value: 1n },
      status: { type: "str", value: "active" },
      valid_until: { type: "u64", value: 2000000000n },
      content_type: { type: "str", value: "application/json" },
      payload: { type: "bytes", value: stringToHex(payloadFixture) },
    },
  };
}

function draft(): OfferDraft {
  return {
    capability: "summarize", priceMicros: "1000000", version: "1",
    validUntil: "2000000000", expiresAtBlock: "4608",
    payload: JSON.parse(payloadFixture) as OfferDraft["payload"],
  };
}

function wallet() {
  const state = { accounts: [owner], chain: "0x76edaf", block: "0x1000", sends: 0 };
  const provider = {
    request: vi.fn(async (args: { method: string; params?: readonly unknown[] | object }): Promise<unknown> => {
      switch (args.method) {
        case "eth_accounts":
        case "eth_requestAccounts": return state.accounts;
        case "eth_chainId": return state.chain;
        case "eth_blockNumber": return state.block;
        case "wallet_switchEthereumChain": state.chain = "0x76edaf"; return null;
        case "eth_sendTransaction": state.sends++; return txHash;
        default: throw new Error(`unexpected test RPC method ${args.method}`);
      }
    }),
  };
  return { state, provider };
}

function options(provider = wallet().provider) {
  return { provider, rpcURL: ARKIV_RPC_URL, expectedAccount: owner, offer: draft(), validateEndpoint: vi.fn(async () => {}) };
}

function guardedTransport() {
  const config = sdk.createClient.mock.lastCall![0] as { transport: Transport };
  return config.transport({ chain: rabdi({ accessKey: "" }) });
}

beforeEach(() => {
  vi.spyOn(Date, "now").mockReturnValue(1900000000000);
  sdk.createClient.mockReset().mockReturnValue({ createEntity: sdk.createEntity });
  sdk.createReader.mockReset().mockReturnValue({ getChainId: sdk.getChainId, getEntity: sdk.getEntity, getBlockNumber: sdk.getBlockNumber });
  sdk.getChainId.mockReset().mockResolvedValue(7794095);
  sdk.getEntity.mockReset().mockResolvedValue(readbackFixture());
  sdk.getBlockNumber.mockReset().mockResolvedValue(4096n);
  sdk.createEntity.mockReset().mockImplementation(async () => {
    await guardedTransport().request({ method: "eth_sendTransaction", params: [{ from: owner, chainId: "0x76edaf" }] });
    return receiptResult;
  });
});
afterEach(() => vi.restoreAllMocks());

describe("provider wallet state", () => {
  it("exposes unconfigured, disconnected, wrong-network and ready states", async () => {
    const { provider, state } = wallet();
    expect((await inspectProviderWallet(provider)).status).toBe("unconfigured");
    expect((await inspectProviderWallet(undefined, ARKIV_RPC_URL)).status).toBe("disconnected");
    state.accounts = [];
    expect((await inspectProviderWallet(provider, ARKIV_RPC_URL)).status).toBe("disconnected");
    state.accounts = [owner];
    state.chain = "0x1";
    expect((await inspectProviderWallet(provider, ARKIV_RPC_URL)).status).toBe("wrong-network");
    state.chain = "0x7614d1";
    expect((await inspectProviderWallet(provider, ARKIV_RPC_URL)).status).toBe("wrong-network");
    state.chain = "0x76edaf";
    expect(await inspectProviderWallet(provider, ARKIV_RPC_URL)).toEqual({ status: "ready", account: owner, chainId: 7794095, currentBlock: "4096" });
  });

  it("awaits the network switch and verifies the actual resulting network", async () => {
    const { provider, state } = wallet();
    state.chain = "0x1";
    expect((await connectProviderWallet(provider, ARKIV_RPC_URL)).status).toBe("ready");
    expect(provider.request).toHaveBeenCalledWith({ method: "eth_requestAccounts" });
    expect(provider.request).toHaveBeenCalledWith({ method: "wallet_switchEthereumChain", params: [{ chainId: "0x76edaf" }] });
    state.chain = "0x1";
    const request = provider.request.getMockImplementation()!;
    provider.request.mockImplementation(async (args) => args.method === "wallet_switchEthereumChain" ? null : request(args));
    await expect(connectProviderWallet(provider, ARKIV_RPC_URL)).rejects.toThrow("Switch the provider wallet");
  });

  it("shows wallet RPC errors without exposing the provider message", async () => {
    const { provider } = wallet();
    provider.request.mockRejectedValue(new Error("secret RPC credential"));
    const state = await inspectProviderWallet(provider, ARKIV_RPC_URL);
    expect(state.status).toBe("error");
    expect(JSON.stringify(state)).not.toContain("credential");
  });
});

describe("public offer schema", () => {
  it("matches the independently authored Go wire payload and preserves u64 amounts", () => {
    const input = draft();
    input.priceMicros = "18446744073709551615";
    const prepared = prepareOffer(input, owner, 1900000000);
    expect(prepared.priceMicros).toBe(18446744073709551615n);
    expect(JSON.stringify(prepared.payload)).toBe(payloadFixture);
  });

  it.each(["0", "01", "1.5", "-1", "1e6", "18446744073709551616"])("rejects noncanonical integer amount %s", (priceMicros) => {
    expect(() => prepareOffer({ ...draft(), priceMicros }, owner, 1900000000)).toThrow();
  });

  it("rejects private fields, remote schemas and mismatched payees", () => {
    const privateInput = draft();
    Object.assign(privateInput.payload, { credentials: "secret" });
    expect(() => prepareOffer(privateInput, owner, 1900000000)).toThrow();
    const schemaInput = draft();
    Object.assign(schemaInput.payload.input_schema.properties.prompt, { default: "private input" });
    expect(() => prepareOffer(schemaInput, owner, 1900000000)).toThrow();
    const payeeInput = draft();
    payeeInput.payload.payee = "0x3333333333333333333333333333333333333333";
    expect(() => prepareOffer(payeeInput, owner, 1900000000)).toThrow();
  });

  it.each(["http://merchant.example/tool", "https://user:pass@merchant.example/tool", "https://merchant.example/tool?secret=value", "https://merchant.example/tool#fragment"])("rejects unsafe URL syntax %s", (endpoint) => {
    const input = draft();
    input.payload.endpoint = endpoint;
    expect(() => prepareOffer(input, owner, 1900000000)).toThrow();
  });
});

describe("owner-signed SDK writes", () => {
  it("runs endpoint preflight and waits for the actual SDK receipt result", async () => {
    let resolve!: (value: typeof receiptResult) => void;
    sdk.createEntity.mockReturnValue(new Promise((done) => { resolve = done; }));
    const config = options();
    let completed = false;
    const pending = publishOffer(config).then((result) => { completed = true; return result; });
    await vi.waitFor(() => expect(sdk.createEntity).toHaveBeenCalledOnce());
    expect(completed).toBe(false);
    expect(config.validateEndpoint).toHaveBeenCalledWith("https://merchant.example/tool");
    const [schema, values, expiry] = sdk.createEntity.mock.calls[0];
    expect(schema.tag).toMatchObject({ type: "str", value: "agentmesh.service-offer.v2" });
    expect(Object.keys(values).sort()).toEqual(["asset", "capability", "content_type", "kind", "payload", "price_micros", "project", "settlement_chain", "status", "valid_until", "version"]);
    expect(values.price_micros).toMatchObject({ type: "u64", value: 1000000n });
    expect(values.asset).toMatchObject({ type: "addr", value: "0x3600000000000000000000000000000000000000" });
    expect(hexToString(values.payload.value)).toBe(payloadFixture);
    expect(expiry).toHaveProperty("expiration");
    await guardedTransport().request({ method: "eth_sendTransaction", params: [{ from: owner }] });
    resolve(receiptResult);
    expect(await pending).toEqual({ entityKey, txHash, expiresAtBlock: "4608", owner });
  });

  it("does not sign when mandatory endpoint policy rejects a private destination", async () => {
    const config = options();
    config.offer.payload.endpoint = "https://127.0.0.1/tool";
    config.validateEndpoint.mockRejectedValue(new Error("private destination"));
    await expect(publishOffer(config)).rejects.toMatchObject({ code: "endpoint-rejected" });
    expect(sdk.createClient).not.toHaveBeenCalled();
  });

  it("does not publish while disconnected, on another chain or after an account switch", async () => {
    const { provider, state } = wallet();
    state.accounts = [];
    await expect(publishOffer(options(provider))).rejects.toMatchObject({ code: "disconnected" });
    state.accounts = [owner];
    state.chain = "0x1";
    await expect(publishOffer(options(provider))).rejects.toMatchObject({ code: "wrong-network" });
    state.chain = "0x7614d1";
    await expect(publishOffer(options(provider))).rejects.toMatchObject({ code: "wrong-network" });
    state.chain = "0x76edaf";
    state.accounts = ["0x3333333333333333333333333333333333333333"];
    await expect(publishOffer(options(provider))).rejects.toMatchObject({ code: "account-changed" });
    expect(sdk.createEntity).not.toHaveBeenCalled();
  });

  it.each(["account", "chain", "legacy-chain", "reader-chain", "block", "UTC"])("rechecks %s at the actual send boundary", async (changed) => {
    const { provider, state } = wallet();
    sdk.createEntity.mockImplementation(async () => {
      if (changed === "account") state.accounts = ["0x3333333333333333333333333333333333333333"];
      if (changed === "chain") state.chain = "0x1";
      if (changed === "legacy-chain") state.chain = "0x7614d1";
      if (changed === "reader-chain") sdk.getChainId.mockResolvedValue(7738577);
      if (changed === "block") state.block = "0x1200";
      if (changed === "UTC") vi.mocked(Date.now).mockReturnValue(2000000000000);
      await guardedTransport().request({ method: "eth_sendTransaction", params: [{ from: owner, chainId: "0x76edaf" }] });
      return receiptResult;
    });
    await expect(publishOffer(options(provider))).rejects.toThrow();
    expect(state.sends).toBe(0);
  });

  it("copies public terms before awaiting preflight", async () => {
    const config = options();
    config.validateEndpoint.mockImplementation(async () => {
      config.offer.payload.endpoint = "https://changed.example/tool";
      config.offer.payload.input_schema.required.push("other");
      config.offer.priceMicros = "999999999";
    });
    await publishOffer(config);
    const values = sdk.createEntity.mock.calls[0][1];
    expect(hexToString(values.payload.value)).toBe(payloadFixture);
    expect(values.price_micros).toMatchObject({ value: 1000000n });
  });

  it("never replaces a rejected write with a fabricated entity", async () => {
    sdk.createEntity.mockRejectedValue(new Error("wallet rejection with secret details"));
    await expect(publishOffer(options())).rejects.toMatchObject({ code: "write-rejected", transactionHash: undefined });
    expect(sdk.createEntity).toHaveBeenCalledOnce();
  });

  it("preserves uncertainty after send and blocks automatic fallback sends", async () => {
    const { provider, state } = wallet();
    sdk.createEntity.mockImplementation(async () => {
      const transport = guardedTransport();
      await transport.request({ method: "eth_sendTransaction", params: [{ from: owner }] });
      await transport.request({ method: "wallet_sendTransaction", params: [{ from: owner }] });
      return receiptResult;
    });
    await expect(publishOffer(options(provider))).rejects.toMatchObject({ code: "write-uncertain", transactionHash: txHash });
    expect(state.sends).toBe(1);
  });

  it("rejects malformed or mismatched confirmed results", async () => {
    sdk.createEntity.mockImplementation(async () => {
      await guardedTransport().request({ method: "eth_sendTransaction", params: [{ from: owner }] });
      return { ...receiptResult, expiresAt: 2000000000n };
    });
    await expect(publishOffer(options())).rejects.toMatchObject({ code: "write-uncertain", transactionHash: txHash });
    sdk.createEntity.mockImplementation(async () => {
      await guardedTransport().request({ method: "eth_sendTransaction", params: [{ from: owner }] });
      return { ...receiptResult, entityKey: "0x1234" };
    });
    await expect(publishOffer(options())).rejects.toMatchObject({ code: "write-uncertain" });
  });

  it("requires an observed send even when the SDK returns plausible result fields", async () => {
    sdk.createEntity.mockResolvedValue(receiptResult);
    await expect(publishOffer(options())).rejects.toMatchObject({ code: "write-uncertain" });
    expect(sdk.getEntity).not.toHaveBeenCalled();
  });

  it("rejects a configured reader on a different chain before sending", async () => {
    const { provider, state } = wallet();
    sdk.getChainId.mockResolvedValue(7738577);
    await expect(publishOffer(options(provider))).rejects.toMatchObject({ code: "wrong-network" });
    expect(state.sends).toBe(0);
    expect(sdk.createEntity).not.toHaveBeenCalled();
  });

  it("waits for independent entity readback after receipt confirmation", async () => {
    let resolve!: (value: ReturnType<typeof readbackFixture>) => void;
    sdk.getEntity.mockReturnValue(new Promise((done) => { resolve = done; }));
    let completed = false;
    const pending = publishOffer(options()).then((result) => { completed = true; return result; });
    await vi.waitFor(() => expect(sdk.getEntity).toHaveBeenCalledWith(entityKey));
    expect(completed).toBe(false);
    resolve(readbackFixture());
    expect(await pending).toEqual({ entityKey, txHash, expiresAtBlock: "4608", owner });
  });

  it.each(["owner", "key", "schema", "version", "payload", "price", "native-expiry", "UTC", "extra", "type", "chain", "account", "read-error", "expired-at-head"])("keeps uncertain %s readback from triggering another write", async (fault) => {
    const { provider, state } = wallet();
    sdk.getEntity.mockImplementation(async () => {
      const entity = readbackFixture();
      if (fault === "owner") entity.$owner = "0x3333333333333333333333333333333333333333";
      if (fault === "key") entity.$key = `0x${"2".repeat(64)}`;
      if (fault === "schema") entity.$attributes["arkiv.schema"].value = "agentmesh.service-offer.v1";
      if (fault === "version") entity.$attributes.version.value = 2n;
      if (fault === "price") entity.$attributes.price_micros.value = 1000001n;
      if (fault === "payload") entity.$attributes.payload.value = stringToHex(payloadFixture + " ");
      if (fault === "native-expiry") entity.$expiresAt = 4609n;
      if (fault === "type") entity.$attributes.payload.type = "str";
      if (fault === "extra") Object.assign(entity.$attributes, { extra: { type: "str", value: "untrusted" } });
      if (fault === "chain") sdk.getChainId.mockResolvedValue(7738577);
      if (fault === "account") state.accounts = ["0x3333333333333333333333333333333333333333"];
      if (fault === "UTC") vi.mocked(Date.now).mockReturnValue(2000000000000);
      if (fault === "expired-at-head") sdk.getBlockNumber.mockResolvedValue(4608n);
      if (fault === "read-error") throw new Error("private RPC detail");
      return entity;
    });
    await expect(publishOffer(options(provider))).rejects.toMatchObject({ code: "write-uncertain", transactionHash: txHash });
    expect(state.sends).toBe(1);
    expect(sdk.createEntity).toHaveBeenCalledOnce();
    expect(sdk.getEntity).toHaveBeenCalledOnce();
  });

  it("encodes the documented Rabdi operation through the actual SDK without network access", async () => {
    const actual = await vi.importActual<typeof import("@arkiv-network/sdk")>("@arkiv-network/sdk");
    sdk.createClient.mockImplementation(actual.createWalletClient);
    const { provider, state } = wallet();
    const request = provider.request.getMockImplementation()!;
    let sent: Record<string, unknown> | undefined;
    provider.request.mockImplementation(async (args) => {
      if (args.method === "eth_sendTransaction") {
        state.sends++;
        sent = (args.params as Record<string, unknown>[])[0];
        throw new Error("controlled send stop");
      }
      return request(args);
    });
    await expect(publishOffer(options(provider))).rejects.toMatchObject({ code: "write-uncertain" });
    expect(state.sends).toBe(1);
    expect(sent?.to).toBe("0x4400000000000000000000000000000000000044");
    expect(sent?.from).toBe(owner);
    const decoded = decodeFunctionData({ abi: parseAbi(["function execute((uint8 operation, bytes operationData)[] ops)"]), data: sent?.data as `0x${string}` });
    expect(decoded.args[0]).toHaveLength(1);
    expect(decoded.args[0][0].operation).toBe(1);
    const [create] = decodeAbiParameters(parseAbiParameters("(uint128 salt, uint64 expiresAt, uint64 minLifetime, uint8 creationFlags, (bytes32 name, uint8 typeId, bytes value)[] attributes)"), decoded.args[0][0].operationData);
    expect(create.expiresAt).toBe(4608n);
    expect(create.minLifetime).toBe(0n);
    expect(create.attributes).toHaveLength(12);
    const attributes = Object.fromEntries(create.attributes.map((entry) => [hexToString(entry.name, { size: 32 }), entry]));
    expect(attributes["arkiv.schema"].typeId).toBe(0x88);
    expect(hexToString(attributes["arkiv.schema"].value)).toBe("agentmesh.service-offer.v2");
    expect(attributes.price_micros.typeId).toBe(0x83);
    expect(BigInt(attributes.price_micros.value)).toBe(1000000n);
    expect(attributes.payload.typeId).toBe(7);
    expect(hexToString(attributes.payload.value)).toBe(payloadFixture);
    expect(attributes.version.typeId).toBe(3);
    expect(BigInt(attributes.version.value)).toBe(1n);
  });
});
