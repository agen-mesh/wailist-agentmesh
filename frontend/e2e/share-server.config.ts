import { randomBytes } from "node:crypto";
import { defineConfig } from "@playwright/test";

const port = Number(process.env.SMOKE_SERVER_PORT ?? 3227);
const nextPort = Number(process.env.SMOKE_NEXT_PORT ?? port + 1);
const visitorBPort = Number(process.env.SMOKE_VISITOR_B_PORT ?? port + 2);
const backend = `http://127.0.0.1:${Number(process.env.SMOKE_API_PORT ?? 3218)}`;
const baseURL = `http://127.0.0.1:${port}`;
const relayKey = process.env.SMOKE_SHARE_READ_RELAY_KEY ?? randomBytes(32).toString("hex");
if (!/^[a-fA-F0-9]{64}$/.test(relayKey)) {
  throw new Error("SMOKE_SHARE_READ_RELAY_KEY must be a disposable 32-byte hex key");
}
process.env.SMOKE_SHARE_READ_RELAY_KEY = relayKey;

export default defineConfig({
  testDir: ".",
  testMatch: "share-server.spec.ts",
  workers: 1,
  retries: 0,
  forbidOnly: !!process.env.CI,
  reporter: "list",
  use: { baseURL, javaScriptEnabled: false, trace: "retain-on-failure" },
  webServer: [
    {
      command: "node support/start-share-backend.mjs",
      url: `${backend}/health`,
      env: { SMOKE_SHARE_READ_RELAY_KEY: relayKey },
      reuseExistingServer: false,
      timeout: 120_000,
    },
    {
      command: `npm run start -- --hostname 127.0.0.1 --port ${nextPort}`,
      cwd: "..",
      url: `http://127.0.0.1:${nextPort}`,
      env: {
        NEXT_PUBLIC_API_URL: "", BACKEND_URL: backend,
        VERCEL: "1", SHARE_READ_RELAY_KEY: relayKey,
      },
      reuseExistingServer: false,
      timeout: 120_000,
    },
    {
      command: "node support/share-visitor-proxy.mjs",
      url: baseURL,
      env: {
        SMOKE_SERVER_PORT: String(port),
        SMOKE_NEXT_PORT: String(nextPort),
        SMOKE_VISITOR_B_PORT: String(visitorBPort),
      },
      reuseExistingServer: false,
      timeout: 120_000,
    },
  ],
});
