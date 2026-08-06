import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  workers: 1,
  timeout: 30_000,
  expect: { timeout: 5_000 },
  use: { baseURL: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000", trace: "retain-on-failure" },
  projects: [
    { name: "chrome-1280", use: { ...devices["Desktop Chrome"], viewport: { width: 1280, height: 800 } } },
    { name: "chrome-1440p", use: { ...devices["Desktop Chrome"], viewport: { width: 2560, height: 1440 } } },
    { name: "chrome-4k", use: { ...devices["Desktop Chrome"], viewport: { width: 3840, height: 2160 } } }
  ]
});
