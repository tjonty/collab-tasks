import { defineConfig, devices } from "@playwright/test";

// The end-to-end test runs against the whole stack, which must be up first:
//   docker compose up --build
//   cd web && npm run test:e2e
// For the demo video: `npm run test:e2e:demo` runs it headed, slowed down,
// and records a video per browser (see test-results/).
const demo = !!process.env.E2E_DEMO;

export default defineConfig({
  testDir: "./e2e",
  globalSetup: "./e2e/global-setup.ts",
  timeout: demo ? 180_000 : 60_000,
  workers: 1,
  reporter: "list",
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:3000",
    trace: "retain-on-failure",
    video: demo ? "on" : "off",
    headless: !demo,
    launchOptions: { slowMo: demo ? 600 : 0 },
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"], viewport: { width: 1280, height: 800 } } }],
});
