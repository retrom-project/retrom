import {expect, type Page, type Route} from "@playwright/test";

export async function verifyExitDuringProviderLoading(page: Page) {
  const pattern = "**/runtime/providers/*/*/client.mjs";
  let release!: () => void;
  const held = new Promise<void>((resolve) => {release = resolve;});
  let moduleRequested!: () => void;
  const requested = new Promise<void>((resolve) => {moduleRequested = resolve;});
  const routeHandler = async (route: Route) => {
    moduleRequested();
    await held;
    await route.continue().catch(() => undefined);
  };
  // Browser requestfailed events can arrive after later requests. Observe the
  // AbortSignal synchronously at the fetch boundary to prove cancellation order.
  await page.evaluate(() => {
    const original = window.fetch;
    const events: string[] = [];
    const record = (event: string) => {
      events.push(event); sessionStorage.setItem("retrom:loading-exit-events", JSON.stringify(events));
    };
    sessionStorage.setItem("retrom:loading-exit-events", "[]");
    window.fetch = (input, init) => {
      const url = typeof input === "string" ? input : input instanceof URL ? input.href : input.url;
      if (/\/runtime\/providers\/[^/]+\/[^/]+\/client\.mjs$/.test(url)) {
        init?.signal?.addEventListener("abort", () => record("module-aborted"), {once: true});
      }
      if (/\/runtime\/launches\/[^/]+\/finish$/.test(url)) {record("finish");}
      if (/\/runtime\/launches\/[^/]+\/start$/.test(url)) {record("start");}
      return original(input, init);
    };
  });
  await page.route(pattern, routeHandler);
  try {
    await page.locator(".library-game-card").filter({hasText: "Sudoku"}).getByRole("link").first().click();
    await page.getByRole("button", {name: "开始游戏"}).click();
    await requested;
    await expect(page.locator(".player-loading")).toBeVisible();
    await page.getByRole("button", {name: "返回并退出游戏"}).click();
    await page.getByRole("alertdialog", {name: "退出游戏？"}).getByRole("button", {name: "退出游戏", exact: true}).click();
    await expect(page).toHaveURL(/\/games\/[0-9a-f-]+$/);
    await expect(page.locator(".player-shell")).toHaveCount(0);
    expect(await page.evaluate(() => JSON.parse(sessionStorage.getItem("retrom:loading-exit-events") ?? "[]")))
      .toEqual(["module-aborted", "finish"]);
    await page.goBack();
    await expect(page).toHaveURL(/\/library$/);
  } finally {
    release();
    await page.unroute(pattern, routeHandler);
  }
}
