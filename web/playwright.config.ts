import { defineConfig, devices } from "@playwright/test";

const chromeExecutablePath = process.env.RETROM_CHROME_EXECUTABLE;

export default defineConfig({
  testDir: "./e2e",
  workers: 1,
  timeout: 30_000,
  expect: { timeout: 5_000 },
  use: {
    baseURL: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000",
    channel: "chrome",
    launchOptions: chromeExecutablePath ? { executablePath: chromeExecutablePath } : undefined,
    trace: "retain-on-failure",
  },
  projects: [
    { name: "chrome-1280", testIgnore: /mobile\.spec\.ts/, use: { ...devices["Desktop Chrome"], viewport: { width: 1280, height: 800 } } },
    { name: "chrome-1440p", testIgnore: /mobile\.spec\.ts/, use: { ...devices["Desktop Chrome"], viewport: { width: 2560, height: 1440 } } },
    { name: "chrome-4k", testIgnore: /mobile\.spec\.ts/, use: { ...devices["Desktop Chrome"], viewport: { width: 3840, height: 2160 } } },
    { name: "chrome-mobile", testMatch: /mobile\.spec\.ts/, use: { ...devices["Pixel 5"], viewport: { width: 390, height: 844 } } }
  ]
});
