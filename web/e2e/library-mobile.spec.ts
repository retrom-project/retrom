import { expect } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";
import { test } from "./clean-refactor-fixtures";

async function readableControl(control: Locator, page: Page) {
  await expect(control).toBeVisible();
  const bounds = await control.boundingBox();
  expect(bounds).not.toBeNull();
  if (!bounds) {
    throw new Error("The control has no rendered bounds.");
  }
  expect(bounds.width).toBeGreaterThanOrEqual(200);
  expect(bounds.x).toBeGreaterThanOrEqual(0);
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(
    page.viewportSize()?.width ?? 390,
  );
}

async function search(page: Page, control: Locator, endpoint: string) {
  const query = "mobile-layout-acceptance";
  const [response] = await Promise.all([
    page.waitForResponse((response) => {
      const url = new URL(response.url());
      return url.pathname === endpoint && url.searchParams.get("q") === query;
    }),
    control.fill(query),
  ]);
  expect(response.status()).toBe(200);
  await expect(control).toHaveValue(query);
}

for (const entry of [
  { route: "/library", heading: "游戏库", endpoint: "/api/v1/games" },
  {
    route: "/admin/reviews",
    heading: "待审核",
    endpoint: "/api/v1/admin/reviews",
  },
  {
    route: "/admin/games",
    heading: "游戏管理",
    endpoint: "/api/v1/admin/games",
  },
]) {
  test(`${entry.heading}: mobile search and filters remain usable`, async ({
    page,
  }) => {
    await page.goto(entry.route);
    await expect(
      page.getByRole("heading", { name: entry.heading, exact: true }),
    ).toBeVisible();
    const input = page.getByRole("textbox", { name: "搜索游戏", exact: true });
    await readableControl(input, page);
    await search(page, input, entry.endpoint);
    await page.getByRole("button", { name: "筛选游戏", exact: true }).click();
    const sheet = page.getByRole("dialog", { name: "筛选游戏", exact: true });
    for (const label of ["游戏目录", "标签", "排序"]) {
      await readableControl(sheet.getByRole("combobox", { name: label }), page);
    }
    const [response] = await Promise.all([
      page.waitForResponse((response) => {
        const url = new URL(response.url());
        return (
          url.pathname === entry.endpoint &&
          url.searchParams.get("sort") === "recent"
        );
      }),
      sheet
        .getByRole("combobox", { name: "排序", exact: true })
        .selectOption("recent"),
    ]);
    expect(response.status()).toBe(200);
    await sheet.getByRole("button", { name: "完成筛选", exact: true }).click();
    await expect(sheet).not.toBeVisible();
  });
}

test("recent: mobile directory and date filters stay inside the viewport", async ({
  page,
}) => {
  await page.goto("/recent");
  await expect(
    page.getByRole("heading", { name: "最近游玩", exact: true }),
  ).toBeVisible();
  const input = page.getByRole("textbox", {
    name: "搜索最近游戏",
    exact: true,
  });
  await readableControl(input, page);
  await readableControl(
    page.getByRole("combobox", { name: "游戏目录", exact: true }),
    page,
  );
  await readableControl(
    page.getByRole("combobox", { name: "排序", exact: true }),
    page,
  );
  await readableControl(page.getByLabel("最后游玩日期之后"), page);
  await search(page, input, "/api/v1/recent-games");
});
