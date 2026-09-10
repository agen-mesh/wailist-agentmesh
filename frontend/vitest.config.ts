import { defineConfig } from "vitest/config";
import path from "path";

export default defineConfig({
  test: {
    environment: "jsdom",
    // .tsx as well as .ts: NotificationsSheet's bug was a state TRANSITION
    // (turn off, then re-read), which is only reachable by driving the
    // component. Pure-function tests could not have caught it.
    include: ["src/**/*.test.{ts,tsx}"],
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
});
