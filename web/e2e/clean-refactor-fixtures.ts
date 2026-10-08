import { readFile } from "node:fs/promises";
import { test as base } from "@playwright/test";
import type { BrowserContext } from "@playwright/test";
import { login } from "./clean-refactor-support";

type Authentication = Awaited<ReturnType<BrowserContext["storageState"]>>;
export const test = base.extend<
  { storageState: Authentication },
  { authentication: Authentication }
>({
  authentication: [
    async ({ browser }, provide) => {
      const statePath = process.env.RETROM_BROWSER_STORAGE_STATE;
      const storageState = statePath
        ? (JSON.parse(await readFile(statePath, "utf8")) as Authentication)
        : undefined;
      const context = await browser.newContext({
        storageState,
        baseURL: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000",
      });
      const page = await context.newPage();
      await login(page);
      const state = await context.storageState();
      await context.close();
      await provide(state);
    },
    { scope: "worker" },
  ],
  storageState: async ({ authentication }, provide) => provide(authentication),
});
