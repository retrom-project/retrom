import {mkdtempSync, rmSync} from "node:fs";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {chromium, type BrowserContext, type TestInfo} from "@playwright/test";

export function persistentContentProfile(testInfo: TestInfo) {
  const directory = mkdtempSync(join(tmpdir(), "retrom-content-profile-"));
  let context: BrowserContext | null = null;
  return {
    async reopen() {
      await context?.close();
      context = await chromium.launchPersistentContext(directory, {
        headless: true, executablePath: process.env.RETROM_CHROME_EXECUTABLE,
        baseURL: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000",
        viewport: testInfo.project.use.viewport, deviceScaleFactor: testInfo.project.use.deviceScaleFactor,
      });
      return context;
    },
    async close() {
      try {await context?.close();} finally {rmSync(directory, {recursive: true, force: true});}
    },
  };
}
