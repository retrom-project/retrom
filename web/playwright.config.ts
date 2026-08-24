import { defineConfig, devices } from "@playwright/test";
import { fileURLToPath } from "node:url";

const chromeExecutablePath = process.env.RETROM_CHROME_EXECUTABLE
  ?? fileURLToPath(new URL("../.cache/tools/retrom-chrome-for-testing", import.meta.url));

// A 3840x2160 display at 150% OS scaling exposes a 2560x1440 CSS viewport.
// Keeping the DPR in the browser context makes screenshots render at the
// physical 3840x2160 pixel size instead of merely resizing a CSS screenshot.
const physical4KAt150Percent = {
  viewport: { width: 2560, height: 1440 },
  screen: { width: 2560, height: 1440 },
  deviceScaleFactor: 1.5,
};

export default defineConfig({
  testDir: "./e2e",
  workers: 1,
  timeout: 30_000,
  expect: { timeout: 5_000 },
  use: {
    baseURL: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000",
    channel: "chrome",
    launchOptions: { executablePath: chromeExecutablePath },
    trace: "retain-on-failure",
  },
  projects: [
    { name: "chrome-1280", testIgnore: /mobile\.spec\.ts/, use: { ...devices["Desktop Chrome"], viewport: { width: 1280, height: 800 } } },
    { name: "chrome-1440p", testIgnore: /mobile\.spec\.ts/, use: { ...devices["Desktop Chrome"], viewport: { width: 2560, height: 1440 } } },
    { name: "chrome-4k-150", testIgnore: /mobile\.spec\.ts/, use: { ...devices["Desktop Chrome"], ...physical4KAt150Percent } },
    { name: "chrome-mobile", testMatch: /mobile\.spec\.ts|immersive\.spec\.ts/, use: { ...devices["Pixel 5"], viewport: { width: 390, height: 844 } } }
  ]
});
