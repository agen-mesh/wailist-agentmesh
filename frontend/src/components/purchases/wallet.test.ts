import { describe, expect, it, vi } from "vitest";
import { signPurchase, switchWalletChain, type BrowserWallet } from "./wallet";
import { PAYER } from "./test-fixtures";

function wallet(account = PAYER, chain = 5042002) {
  const request = vi.fn<BrowserWallet["request"]>(async ({ method }) => {
    if (method === "eth_accounts") return [account];
    if (method === "eth_chainId") return `0x${chain.toString(16)}`;
    if (method === "eth_signTypedData_v4") return `0x${"a".repeat(130)}`;
    throw new Error("Unexpected wallet request");
  });
  return { request } satisfies BrowserWallet;
}

describe("purchase wallet handoff", () => {
  it("does not open signing for the wrong active account or network", async () => {
    for (const provider of [wallet(`0x${"3".repeat(40)}`), wallet(PAYER, 1)]) {
      await expect(signPurchase(provider, PAYER, 5042002, {})).rejects.toThrow();
      expect(provider.request.mock.calls.some(([args]) => args.method === "eth_signTypedData_v4")).toBe(false);
    }
  });

  it("rechecks the account after the wallet prompt before returning its signature", async () => {
    const provider = wallet();
    provider.request.mockImplementation(async ({ method }) => {
      if (method === "eth_accounts") return provider.request.mock.calls.some(([args]) => args.method === "eth_signTypedData_v4") ? [`0x${"3".repeat(40)}`] : [PAYER];
      if (method === "eth_chainId") return "0x4cef52";
      return `0x${"a".repeat(130)}`;
    });
    await expect(signPurchase(provider, PAYER, 5042002, {})).rejects.toThrow("account changed");
  });

  it("adds a missing chain with the server explorer", async () => {
    const provider = wallet(PAYER, 1);
    let added = false;
    provider.request.mockImplementation(async ({ method, params }) => {
      if (method === "eth_chainId") return added ? "0x4cef52" : "0x1";
      if (method === "wallet_switchEthereumChain") {
        if (!added) {
          const missing = new Error("missing chain") as Error & { code: number };
          missing.code = 4902;
          throw missing;
        }
        return null;
      }
      if (method === "wallet_addEthereumChain") {
        added = true;
        return params;
      }
      throw new Error(method);
    });
    await switchWalletChain(provider, 5042002, "https://rpc.testnet.arc.io", "https://explorer.testnet.arc.io/tx");
    const add = provider.request.mock.calls.find(([args]) => args.method === "wallet_addEthereumChain");
    const chain = (add?.[0].params as [{ blockExplorerUrls?: string[] }])?.[0];
    expect(chain?.blockExplorerUrls).toEqual(["https://explorer.testnet.arc.io"]);
    expect(JSON.stringify(add)).not.toContain("arcscan");
  });

  it("preserves an explicit rejected network switch", async () => {
    const provider = wallet(PAYER, 1);
    await expect(switchWalletChain(provider, 5042002)).rejects.toThrow();
    expect(provider.request.mock.calls.some(([args]) => args.method === "wallet_addEthereumChain")).toBe(false);
  });

  it("returns only an EOA signature after successful account and chain checks", async () => {
    const provider = wallet();
    await expect(signPurchase(provider, PAYER, 5042002, { message: { value: "10000" } })).resolves.toBe(`0x${"a".repeat(130)}`);
    expect(provider.request.mock.calls.filter(([args]) => args.method === "eth_accounts")).toHaveLength(2);
  });
});
