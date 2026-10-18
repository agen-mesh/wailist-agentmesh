import { spawn, spawnSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { createServer } from "node:http";
import { resolve } from "node:path";

function required(name) {
  const value = process.env[name];
  if (!value) throw new Error(`${name} must point to the scratch test input`);
  return value;
}

function port(name, fallback) {
  const value = Number(process.env[name] ?? fallback);
  if (!Number.isInteger(value) || value < 1024 || value > 65535) {
    throw new Error(`Invalid ${name}`);
  }
  return value;
}

const database = required("SMOKE_DATABASE_URL");
const databaseURL = new URL(database);
if (!['postgres:', 'postgresql:'].includes(databaseURL.protocol) ||
    !['127.0.0.1', 'localhost'].includes(databaseURL.hostname)) {
  throw new Error("The sharing server test requires a local scratch database");
}
const backendBinary = resolve(required("SMOKE_BACKEND_BINARY"));
const walletBinary = resolve(required("SMOKE_WALLETGEN_BINARY"));
const backendPort = port("SMOKE_API_PORT", 3218);
const frontendPort = port("SMOKE_SERVER_PORT", 3227);
const relayKey = required("SMOKE_SHARE_READ_RELAY_KEY");
if (!/^[a-fA-F0-9]{64}$/.test(relayKey)) {
  throw new Error("SMOKE_SHARE_READ_RELAY_KEY must be a disposable 32-byte hex key");
}
const inertPort = port("SMOKE_INERT_PORT", 3219);
const inertURL = `http://127.0.0.1:${inertPort}`;
const env = Object.fromEntries(
  ["PATH", "HOME", "USER", "LANG"].flatMap((name) =>
    process.env[name] ? [[name, process.env[name]]] : []),
);
const encryptionKey = randomBytes(16).toString("hex");
const wallet = spawnSync(walletBinary, [
  "-enc-key", encryptionKey, "-skip-opt-in", "-network", "testnet",
  "-algod-url", inertURL,
], { encoding: "utf8", env, timeout: 10_000 });
const address = /Address: ([A-Z2-7]{58})/.exec(wallet.stderr ?? "")?.[1];
if (wallet.status !== 0 || !address || !wallet.stdout.trim()) {
  throw new Error("Disposable offline wallet setup failed");
}

const inert = createServer((request, response) => {
  const catalog = request.method === "GET" && request.url?.startsWith("/discovery/resources");
  response.writeHead(catalog ? 200 : 503, { "Content-Type": "application/json" });
  response.end(JSON.stringify(catalog ? { items: [] } : { error: "External calls disabled in smoke tests" }));
});
inert.on("connect", (_request, socket) => socket.end("HTTP/1.1 503 Service Unavailable\r\n\r\n"));
await new Promise((done, reject) => {
  inert.once("error", reject);
  inert.listen(inertPort, "127.0.0.1", done);
});

const backend = spawn(backendBinary, [], {
  cwd: process.env.RUNNER_TEMP ?? process.cwd(),
  stdio: "inherit",
  env: {
    ...env,
    DATABASE_URL: database,
    ENCRYPTION_KEY: encryptionKey,
    JWT_SECRET: randomBytes(32).toString("hex"),
    SHARE_READ_RELAY_KEY: relayKey,
    PLATFORM_WALLET_ADDRESS: address,
    PLATFORM_WALLET_ENC_MNEMONIC: wallet.stdout.trim(),
    PLATFORM_SPEND_WALLET_ADDRESS: address,
    PLATFORM_SPEND_WALLET_ENC_MNEMONIC: wallet.stdout.trim(),
    CASHFREE_APP_ID: "scratch-disabled",
    CASHFREE_SECRET_KEY: "scratch-disabled",
    NOWPAYMENTS_API_KEY: "scratch-disabled",
    NOWPAYMENTS_IPN_SECRET: "scratch-disabled",
    CASHFREE_SANDBOX: "true",
    NOWPAYMENTS_SANDBOX: "true",
    ALGORAND_NETWORK: "testnet",
    ALGOD_URL: inertURL,
    ALGORAND_INDEXER_URL: inertURL,
    FACILITATOR_URL: inertURL,
    TENDRIL_REGISTRY_URL: inertURL,
    BAZAAR_BASE_URL: inertURL,
    RELAY_BASE_URL: inertURL,
    HTTP_PROXY: inertURL,
    HTTPS_PROXY: inertURL,
    NO_PROXY: "127.0.0.1,localhost",
    BASE_URL: `http://127.0.0.1:${backendPort}`,
    FRONTEND_URL: `http://127.0.0.1:${frontendPort}`,
    PORT: String(backendPort),
    GOMAXPROCS: "2",
    GOMEMLIMIT: "512MiB",
  },
});
let stopping = false;
const stop = () => {
  stopping = true;
  backend.kill("SIGTERM");
  inert.close();
};
process.once("SIGTERM", stop);
process.once("SIGINT", stop);
backend.once("error", () => {
  console.error("Scratch backend failed to start");
  inert.close();
  process.exitCode = 1;
});
backend.once("exit", (code) => {
  inert.close();
  process.exitCode = stopping ? 0 : (code || 1);
});
