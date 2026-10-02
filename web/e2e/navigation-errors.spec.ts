import { expect, test } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  const response = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" }, headers: { Origin: origin },
  });
  expect(response.ok()).toBe(true);
});

test("ACC-UI-001 admin to home soft navigation preserves the document without application errors", async ({ page }, testInfo) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.stack ?? error.message));
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "今天，玩点什么？" })).toBeVisible();
  const timeOrigin = await page.evaluate(() => performance.timeOrigin);

  await page.getByRole("link", { name: "管理后台" }).click();
  await expect(page).toHaveURL(/\/admin\/imports$/);
  await expect(page.getByRole("heading", { name: "游戏入库" })).toBeVisible();
  await page.getByRole("link", { name: "返回用户侧" }).click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { name: "今天，玩点什么？" })).toBeVisible();
  await page.goBack();
  await expect(page).toHaveURL(/\/admin\/imports$/);
  await page.goForward();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { name: "今天，玩点什么？" })).toBeVisible();
  await page.evaluate(() => new Promise<void>(resolve => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
  }));

  expect(await page.evaluate(() => performance.timeOrigin)).toBe(timeOrigin);
  expect(errors).toEqual([]);
  await testInfo.attach("navigation-error-boundary", {
    body: JSON.stringify({ browserVersion: page.context().browser()?.version(), routes: ["/", "/admin/imports", "/", "/admin/imports", "/"], errors }),
    contentType: "application/json",
  });
});

test("ACC-UI-001 document bootstrap never cancels an error based on a DevTools-shaped stack", async ({ page }) => {
  await page.goto("/");
  const observation = await page.evaluate(() => {
    const error = new TypeError("Cannot read properties of undefined (reading 'startTime')");
    // This tests error visibility, not reproduction of the browser's INP bug.
    error.stack = `${error.name}: ${error.message}\n    at et.reportAllChanges (<anonymous>:2:19429)\n    at n.timeout (<anonymous>:2:5652)`;
    const event = new ErrorEvent("error", { error, message: error.message, cancelable: true });
    let reachedListener = false;
    const listener = () => { reachedListener = true; };
    window.addEventListener("error", listener);
    try {
      const accepted = window.dispatchEvent(event);
      return { accepted, reachedListener, defaultPrevented: event.defaultPrevented };
    } finally { window.removeEventListener("error", listener); }
  });
  expect(observation).toEqual({ accepted: true, reachedListener: true, defaultPrevented: false });
});

test("ACC-UI-001 library favorites and saves restore filters on cached Back and Forward navigation", async ({page}) => {
  test.setTimeout(90_000);
  for (const scenario of [
    {route: "/library", search: "搜索游戏", sort: "排列顺序", value: "TITLE_ASC"},
    {route: "/favorites", search: "搜索收藏", sort: "排序方式", value: "TITLE_ASC"},
    {route: "/saves", search: "搜索", sort: "排列", value: "CREATED_ASC"},
  ]) {
    await page.goto(scenario.route);
    const timeOrigin = await page.evaluate(() => performance.timeOrigin);
    const search = page.getByRole("searchbox", {name: scenario.search, exact: true});
    const sort = page.getByRole("combobox", {name: scenario.sort, exact: true});
    await search.fill("old-query");
    await search.fill("latest-query");
    await sort.selectOption(scenario.value);
    await expect(page).toHaveURL(/q=latest-query/);
    await expect(page).toHaveURL(new RegExp(`sort=${scenario.value}`));
    const filteredURL = page.url();
    await page.getByRole("navigation", {name: "主要导航"}).getByRole("link", {name: "首页", exact: true}).click();
    await expect(page).toHaveURL(/\/$/);
    await page.goBack();
    await expect(page).toHaveURL(filteredURL);
    await expect(search).toHaveValue("latest-query");
    await expect(sort).toHaveValue(scenario.value);
    // Wait beyond both feature debounces: a stale effect must not rewrite Back.
    await page.waitForTimeout(400);
    await expect(page).toHaveURL(filteredURL);
    await page.goForward();
    await expect(page).toHaveURL(/\/$/);
    await page.goBack();
    await expect(search).toHaveValue("latest-query");
    await expect(sort).toHaveValue(scenario.value);
    expect(await page.evaluate(() => performance.timeOrigin)).toBe(timeOrigin);
  }
});
