import { defineConfig, devices } from "@playwright/test";

// Smoke tests against a production build of the site in the parent directory.
// CI builds it with NEXT_PUBLIC_API_URL unset, so lib/api.ts serves its
// fixtures and no backend is needed.
//
// This is a separate npm project on purpose. Adding @playwright/test to the
// site's own package.json re-resolves peer dependencies across the site's
// pnpm-lock.yaml, which Vercel installs from. Keeping the test tooling here
// leaves the website's dependency graph untouched.
const PORT = Number(process.env.SMOKE_PORT ?? 3217);
const baseURL = `http://127.0.0.1:${PORT}`;

// A reduced Android Chrome user agent, so lib/device.ts classifies the page as
// a handheld the way it would on a phone.
const ANDROID_UA =
  "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Mobile Safari/537.36";

const android = (width: number, height: number, deviceScaleFactor: number) => ({
  viewport: { width, height },
  deviceScaleFactor,
  isMobile: true,
  hasTouch: true,
  userAgent: ANDROID_UA,
});

export default defineConfig({
  testDir: ".",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["list"]] : "list",
  use: { baseURL, trace: "retain-on-failure" },
  projects: [
    {
      name: "desktop-1440",
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 900 },
      },
    },
    { name: "android-360", use: android(360, 800, 3) },
    { name: "android-320", use: android(320, 640, 2) },
  ],
  webServer: {
    command: `npm run start -- --port ${PORT}`,
    cwd: "..",
    url: baseURL,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
