export interface BrowserWallet {
  request(args: { method: string; params?: readonly unknown[] | object }): Promise<unknown>;
  on?(event: string, listener: (...args: unknown[]) => void): void;
  removeListener?(event: string, listener: (...args: unknown[]) => void): void;
}

const ADDRESS = /^0x[0-9a-fA-F]{40}$/;

export function browserWallet(): BrowserWallet | null {
  if (typeof window === "undefined") return null;
  const candidate = (window as unknown as { ethereum?: BrowserWallet }).ethereum;
  return candidate && typeof candidate.request === "function" ? candidate : null;
}

export async function walletAccount(wallet: BrowserWallet, prompt = false): Promise<string> {
  const accounts = await wallet.request({ method: prompt ? "eth_requestAccounts" : "eth_accounts" });
  if (!Array.isArray(accounts) || typeof accounts[0] !== "string" || !ADDRESS.test(accounts[0])) {
    throw new Error("Connect an EVM wallet account to continue.");
  }
  return accounts[0];
}

export async function walletChain(wallet: BrowserWallet): Promise<number> {
  const value = await wallet.request({ method: "eth_chainId" });
  if (typeof value !== "string" || !/^0x[0-9a-f]+$/i.test(value)) {
    throw new Error("The wallet did not report a valid network.");
  }
  const chain = Number(BigInt(value));
  if (!Number.isSafeInteger(chain)) throw new Error("Unsupported wallet network.");
  return chain;
}

export async function checkWallet(wallet: BrowserWallet, account: string, chainId: number): Promise<void> {
  const current = await walletAccount(wallet);
  if (current.toLowerCase() !== account.toLowerCase()) {
    throw new Error("The wallet account changed. Reconnect the account linked to this order.");
  }
  if (await walletChain(wallet) !== chainId) {
    throw new Error(`Switch your wallet to chain ${chainId} before continuing.`);
  }
}

export async function switchWalletChain(wallet: BrowserWallet, chainId: number, rpcURL?: string): Promise<void> {
  if (await walletChain(wallet) === chainId) return;
  const chainHex = `0x${chainId.toString(16)}`;
  try {
    await wallet.request({ method: "wallet_switchEthereumChain", params: [{ chainId: chainHex }] });
  } catch (error) {
    if (!error || typeof error !== "object" || !("code" in error) || error.code !== 4902 || chainId !== 5042002 || !rpcURL) throw error;
    const rpc = new URL(rpcURL);
    if (rpc.protocol !== "https:" || rpc.username || rpc.password || rpc.search || rpc.hash) {
      throw new Error("A public Arc testnet RPC is needed to add this network to the wallet.");
    }
    await wallet.request({ method: "wallet_addEthereumChain", params: [{
      chainId: chainHex, chainName: "Arc Testnet", rpcUrls: [rpc.href],
      nativeCurrency: { name: "USDC", symbol: "USDC", decimals: 18 },
      blockExplorerUrls: ["https://testnet.arcscan.app"],
    }] });
    await wallet.request({ method: "wallet_switchEthereumChain", params: [{ chainId: chainHex }] });
  }
  if (await walletChain(wallet) !== chainId) throw new Error("The wallet network did not change.");
}

export async function signWalletChallenge(wallet: BrowserWallet, account: string, message: string): Promise<string> {
  if (!message || message.length > 8192) throw new Error("The wallet-link challenge is invalid.");
  if ((await walletAccount(wallet)).toLowerCase() !== account.toLowerCase()) {
    throw new Error("The wallet account changed before linking.");
  }
  const bytes = new TextEncoder().encode(message);
  const hex = `0x${Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")}`;
  return requireSignature(await wallet.request({ method: "personal_sign", params: [hex, account] }));
}

export async function signPurchase(wallet: BrowserWallet, account: string, chainId: number, typedData: unknown): Promise<string> {
  await checkWallet(wallet, account, chainId);
  const signature = requireSignature(await wallet.request({ method: "eth_signTypedData_v4", params: [account, JSON.stringify(typedData)] }));
  await checkWallet(wallet, account, chainId);
  return signature;
}

function requireSignature(value: unknown): string {
  if (typeof value !== "string" || !/^0x[0-9a-fA-F]{130}$/.test(value)) {
    throw new Error("This purchase requires an EOA wallet with a 65-byte signature.");
  }
  return value;
}
