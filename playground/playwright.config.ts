import { defineConfig, devices } from "@playwright/test";

const playgroundPort = Number(process.env.PLAYGROUND_E2E_PORT ?? 8400);
const playgroundURL = `http://127.0.0.1:${playgroundPort}`;

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  timeout: 60_000,
  use: {
    baseURL: playgroundURL,
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
    video: "retain-on-failure",
  },
  webServer: {
    command: "node ./e2e/serve.ts",
    url: playgroundURL,
    reuseExistingServer: false,
    timeout: 15 * 60_000,
    gracefulShutdown: { signal: "SIGTERM", timeout: 10_000 },
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
