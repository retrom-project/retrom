import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import type { Schema } from "../lib/api/types";

export const acceptanceTitle =
  process.env.RETROM_BROWSER_GAME_TITLE ?? "Browser acceptance NES";
const username = process.env.RETROM_BROWSER_ADMIN_USER ?? "test";
const password = process.env.RETROM_BROWSER_ADMIN_PASSWORD ?? "test";

export async function login(page: Page) {
  await page.goto("/login");
  const auth = await request<Schema<"AuthContext">>(
    page,
    "/api/v1/auth/context",
  );
  if (auth.user) {
    await expect(page).toHaveURL(/\/$/u);
    return;
  }
  if (!auth.initialized) {
    await page.goto("/setup");
    await page.getByLabel("账号", { exact: true }).fill(username);
    await page
      .getByLabel("显示名称", { exact: true })
      .fill("Browser administrator");
    await page.getByLabel("密码", { exact: true }).fill(password);
  } else {
    await page.getByLabel("用户名", { exact: true }).fill(username);
    await page.getByLabel("密码", { exact: true }).fill(password);
  }
  await page.locator("form").getByRole("button").click();
  await expect(page).toHaveURL(/\/$/u);
}

export async function request<T>(
  page: Page,
  path: string,
  body?: unknown,
  method = body ? "POST" : "GET",
): Promise<T> {
  const response = await page.evaluate(
    async ({ path, body, method }) => {
      const auth = (await (await fetch("/api/v1/auth/context")).json()) as {
        csrfToken: string;
      };
      const response = await fetch(path, {
        method,
        headers: body
          ? {
              "Content-Type": "application/json",
              "X-Retrom-Csrf": auth.csrfToken,
            }
          : {},
        ...(body ? { body: JSON.stringify(body) } : {}),
      });
      return {
        status: response.status,
        data:
          response.status === 204 ? null : ((await response.json()) as unknown),
      };
    },
    { path, body, method },
  );
  expect(response.status, `API ${method} ${path}`).toBeLessThan(300);
  return response.data as T;
}

export async function prepareLibrary(page: Page): Promise<Schema<"Game">> {
  const directories = await request<Schema<"DirectoryList">>(
    page,
    "/api/v1/admin/platform-instances",
  );
  const existing = directories.items.find(
    (directory) => directory.slug === "browser-acceptance",
  );
  const directory =
    existing ??
    (await request<Schema<"Directory">>(
      page,
      "/api/v1/admin/platform-instances",
      {
        name: "Browser acceptance",
        slug: "browser-acceptance",
        description: "Project-owned MIT NES browser fixture",
        platformId: "nes",
        defaultCoreId: "fceumm",
        coreIds: ["fceumm"],
        enabled: true,
      },
    ));
  const sourcePath = process.env.RETROM_BROWSER_SOURCE_PATH ?? "/pfb-workspace/acceptance/sources/browser";
  await page.goto("/admin/imports/server");
  await page.getByRole("button", { name: "选择 Pegasus 目录", exact: true }).click();
  await page.getByLabel("服务器目录", { exact: true }).fill(sourcePath);
  await page.getByRole("button", { name: "进入目录", exact: true }).click();
  await page.getByRole("button", { name: "读取来源集合", exact: true }).click();
  await page
    .locator('select[aria-label$="游戏目录"]')
    .selectOption(directory.id);
  const scanResponse = page.waitForResponse(
    (response) =>
      response.url().endsWith("/api/v1/admin/game-scans") &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "开始扫描", exact: true }).click();
  const scan = (await (await scanResponse).json()) as Schema<"ScanProgress">;
  await expect(page).toHaveURL(new RegExp(`/admin/reviews\\?scanId=${scan.id}$`, "u"));
  await expect(page.getByRole("region", { name: "当前游戏扫描", exact: true })).toBeVisible();
  await expect
    .poll(async () => {
      const scans = await request<Schema<"ScanList">>(
        page,
        "/api/v1/admin/scans",
      );
      const progress = scans.items.find((item) => item.id === scan.id);
      if (!progress) {
        return "not_listed";
      }
      expect(progress.failedCount).toBe(0);
      return progress.status;
    })
    .toBe("completed");
  await expect(page.getByRole("region", { name: "当前游戏扫描", exact: true }).getByRole("status")).toContainText("扫描已完成");
  const games = await request<Schema<"GamePage">>(
    page,
    `/api/v1/admin/games?platformInstanceId=${directory.id}&q=${encodeURIComponent(acceptanceTitle)}`,
  );
  const reviews = await request<Schema<"GamePage">>(
    page,
    `/api/v1/admin/reviews?platformInstanceId=${directory.id}&q=${encodeURIComponent(acceptanceTitle)}`,
  );
  const game = [...games.items, ...reviews.items].find(
    (item) => item.title === acceptanceTitle,
  );
  expect(game).toBeDefined();
  if (!game) {
    throw new Error("The legal browser fixture was not imported.");
  }
  if (game.status === "pending_review") {
    await page.goto(`/admin/reviews/${game.id}`);
    await page.getByRole("button", { name: "通过并发布", exact: true }).click();
    await expect(page).toHaveURL(/\/admin\/reviews$/u);
    await expect(page.getByRole("heading", { name: "没有待审核的游戏", exact: true })).toBeVisible();
  }
  return game;
}

export async function fixtureGame(page: Page): Promise<Schema<"Game">> {
  const games = await request<Schema<"GamePage">>(
    page,
    `/api/v1/games?q=${encodeURIComponent(acceptanceTitle)}`,
  );
  const game = games.items.find(
    (item) =>
      item.title === acceptanceTitle &&
      item.directoryName === "Browser acceptance",
  );
  if (!game) {
    throw new Error("Run the library acceptance case first.");
  }
  return game;
}

export async function leavePlayer(page: Page) {
  await revealPlayerControls(page);
  await page.getByRole("button", { name: "更多操作", exact: true }).click();
  await page.getByRole("menuitem", { name: "退出游戏", exact: true }).click();
  await page
    .getByRole("alertdialog")
    .getByRole("button", { name: "退出游戏", exact: true })
    .click();
  await expect(page).not.toHaveURL(/\/play\//u);
}

export async function revealPlayerControls(page: Page) {
  for (const frame of page.frames()) {
    await frame.evaluate(() => document.exitPointerLock?.());
  }
  const toolbar = page.locator(".player-toolbar");
  const handle = page.getByRole("button", { name: "显示游戏工具栏", exact: true });
  await expect(async () => {
    if (await handle.isVisible()) { await handle.click(); }
    // A just-closed save sheet can release its pinned HUD between two frames.
    // Real pointer hover keeps the visible toolbar available for the next action.
    await toolbar.getByRole("button", { name: "返回并退出游戏", exact: true }).hover({ timeout: 1500 });
    await expect(toolbar).toHaveClass(/is-visible/u);
    await expect(toolbar).toBeInViewport();
  }).toPass({ timeout: 10_000, intervals: [100] });
}

export async function verifyLibraryFilters(page: Page) {
  const game = await fixtureGame(page);
  const catalog = await request<Schema<"RuntimeCatalog">>(
    page,
    "/api/v1/runtime/catalog",
  );
  const platform = catalog.platforms.find(
    (item) => item.id === game.platformId,
  );
  if (!platform) {
    throw new Error("The fixture platform declaration is missing.");
  }
  await page.goto("/library?offset=24");
  const mobileFilters = page.getByRole("button", {
    name: "筛选游戏",
    exact: true,
  });
  if (await mobileFilters.isVisible()) {
    await mobileFilters.click();
  }
  await page
    .getByRole("combobox", { name: "排序", exact: true })
    .selectOption("recent");
  const completeFilters = page.getByRole("button", {
    name: "完成筛选",
    exact: true,
  });
  if (await completeFilters.isVisible()) {
    await completeFilters.click();
  }
  await expect
    .poll(() => new URL(page.url()).searchParams.get("offset"))
    .toBeNull();
  const filtered = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return (
      url.pathname === "/api/v1/games" &&
      url.searchParams.get("platformId") === game.platformId
    );
  });
  await page
    .locator(".library-platform-row")
    .getByRole("button", { name: platform.name, exact: true })
    .click();
  const response = await filtered;
  expect(response.status()).toBe(200);
  const data = (await response.json()) as Schema<"GamePage">;
  expect(data.items.length).toBeGreaterThan(0);
  expect(data.items.every((item) => item.platformId === game.platformId)).toBe(
    true,
  );
  expect(new URL(response.url()).searchParams.get("sort")).toBe("recent");
  expect(new URL(response.url()).searchParams.get("offset")).toBe("0");
  const cover = page.locator(".library-game-cover").first();
  const heart = cover.locator(".favorite-heart");
  await expect(heart).toBeVisible();
  const circles = await heart.evaluate((button) => ({
    radius: getComputedStyle(button).borderRadius,
    width: button.getBoundingClientRect().width,
    height: button.getBoundingClientRect().height,
  }));
  expect(circles.radius).toBe("50%");
  expect(circles.width).toBe(circles.height);
}
