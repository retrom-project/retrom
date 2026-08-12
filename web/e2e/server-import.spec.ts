import { mkdirSync } from "node:fs";
import { execFileSync } from "node:child_process";
import path from "node:path";
import axe from "axe-core";
import { expect, test, type Page, type TestInfo } from "@playwright/test";

test.beforeAll(() => {
  if (process.env.E2E_SERVER_IMPORT_SEED !== "1") return;
  const database = process.env.RETROM_E2E_DATABASE;
  if (!database) throw new Error("RETROM_E2E_DATABASE is required for the server import E2E fixture");
  execFileSync("python3", [path.resolve(process.cwd(), "../scripts/acceptance/seed-bios-catalog.py"), database, "286"], { stdio: "inherit" });
});

test.beforeEach(async ({ page }, testInfo) => {
  if (testInfo.title.startsWith("ACC-BIOS-007") && testInfo.project.name !== "chrome-1280") {
    test.skip(true, "FULL_CATALOG 行为只消费一次共享 catalog；多尺寸由 ACC-BIOS-006 覆盖");
  }
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000";
  const response = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" }, headers: { Origin: origin },
  });
  expect(response.ok()).toBe(true);
});

function evidencePath(testInfo: TestInfo, name: string) {
  const caseDirectory = process.env.RETROM_ACCEPTANCE_CASE_DIR;
  if (!caseDirectory) return testInfo.outputPath(name);
  const screenshots = path.join(caseDirectory, "screenshots");
  mkdirSync(screenshots, { recursive: true });
  return path.join(screenshots, `${testInfo.project.name}-${name}`);
}

async function expectNoPageOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
}

async function expectNoSeriousAxeViolations(page: Page) {
  await page.evaluate(axe.source);
  const violations = await page.evaluate(async () => {
    const axeAPI = (window as typeof window & { axe: { run: (root: Document, options: object) => Promise<{ violations: Array<{ id: string; impact: string | null; nodes: unknown[] }> }> } }).axe;
    const result = await axeAPI.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    return result.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical");
  });
  expect(violations, `axe serious/critical violations: ${JSON.stringify(violations)}`).toEqual([]);
}

test("ACC-BIOS-006 server import drawer, recovery detail, keyboard and desktop layouts", async ({ page }, testInfo) => {
  await page.goto("/admin/imports/server");
  await expect(page.getByRole("heading", { name: "从服务器目录导入" })).toBeVisible();
  await expect(page.getByText("Pegasus BIOS", { exact: true }).first()).toBeVisible();
  await expectNoPageOverflow(page);

  const trigger = page.getByRole("button", { name: "选择目录并开始" });
  await trigger.focus();
  await trigger.click();
  const drawer = page.getByRole("dialog", { name: "选择 BIOS 所在目录" });
  await expect(drawer).toBeVisible();
  await expect(page.getByRole("checkbox", { name: /允许使用更优候选替换已有 BIOS/ })).not.toBeChecked();
  await expect(drawer).not.toContainText("/tmp/");
  await page.keyboard.press("Shift+Tab");
  expect(await drawer.evaluate((element) => element.contains(document.activeElement))).toBe(true);
  await page.keyboard.press("Escape");
  await expect(drawer).toHaveCount(0);
  await expect(trigger).toBeFocused();

  await trigger.click();
  await page.getByRole("button", { name: /^BIOS/ }).click();
  await expect(drawer.getByText("Pegasus BIOS / BIOS", { exact: true })).toBeVisible();
  await drawer.getByRole("button", { name: "开始异步导入" }).click();
  await expect(page).toHaveURL(/\/admin\/imports\/server\/[0-9a-f-]+$/);
  await expect(page.getByText("已完成", { exact: true }).first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("region", { name: "服务器导入摘要" })).toBeVisible();
  await expect(page.getByRole("table", { name: "BIOS 导入结果" })).toBeVisible();
  await expectNoPageOverflow(page);
  await expectNoSeriousAxeViolations(page);
  await page.screenshot({ path: evidencePath(testInfo, "server-bios-import-detail.png"), fullPage: true });
});

test("ACC-BIOS-007 FULL_CATALOG traverses 100/100/86 and retries the same cursor", async ({ page }, testInfo) => {
  const sizes: number[] = [];
  const ids = new Set<string>();
  let cursor: string | null = null;
  do {
    const query = new URLSearchParams({ scope: "FULL_CATALOG", limit: "100" });
    if (cursor) query.set("cursor", cursor);
    const response = await page.request.get(`/api/v1/admin/bios?${query}`);
    expect(response.ok()).toBe(true);
    const payload = await response.json() as { items: Array<{ id: string }>; nextCursor: string | null; filteredCount: number; summary: { totalCount: number } };
    sizes.push(payload.items.length);
    payload.items.forEach((item) => ids.add(item.id));
    expect(payload.filteredCount).toBe(286);
    expect(payload.summary.totalCount).toBe(286);
    cursor = payload.nextCursor;
  } while (cursor);
  expect(sizes).toEqual([100, 100, 86]);
  expect(ids.size).toBe(286);

  let failedOnce = false;
  const cursorRequests: string[] = [];
  await page.route((url) => url.pathname === "/api/v1/admin/bios" && url.searchParams.has("cursor"), async (route) => {
    cursorRequests.push(route.request().url());
    if (!failedOnce) {
      failedOnce = true;
      await route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: { code: "INTERNAL_ERROR", message: "injected next-page failure" } }) });
      return;
    }
    await route.continue();
  });
  await page.goto("/admin/bios?scope=FULL_CATALOG");
  await expect(page.getByText("已加载 100 / 286 项", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "加载更多" }).click();
  await expect(page.getByRole("button", { name: "重试加载下一页" })).toBeVisible();
  await expect(page.getByText("已加载 100 / 286 项", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "重试加载下一页" }).click();
  await expect(page.getByText("已加载 200 / 286 项", { exact: true })).toBeVisible();
  expect(new URL(cursorRequests[0]).searchParams.get("cursor")).toBe(new URL(cursorRequests[1]).searchParams.get("cursor"));
  await page.getByRole("button", { name: "加载更多" }).click();
  await expect(page.getByText("已加载全部 286 项", { exact: true })).toBeVisible();
  await expect(page.getByRole("row")).toHaveCount(286);
  await expectNoPageOverflow(page);
  await page.screenshot({ path: evidencePath(testInfo, "bios-full-catalog-286.png"), fullPage: true });
});

test("ACC-PEG-005 three-step Pegasus import recovers and remains bounded at desktop viewports", async ({ page }, testInfo) => {
  test.setTimeout(120_000);
  await page.goto("/admin/imports/server");
  await expect(page.getByRole("heading", { name: "扫描并导入 BIOS" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "扫描并导入游戏" })).toBeVisible();
  await expect(page.getByText("Pegasus BIOS", { exact: true }).first()).toBeVisible();
  await expectNoPageOverflow(page);

  const trigger = page.getByRole("button", { name: /选择目录并扫描|继续扫描或映射/ });
  await trigger.click();
  let drawer = page.getByRole("dialog", { name: "从 Pegasus 目录导入游戏" });
  await expect(drawer).toBeVisible();
  await expect(drawer.getByRole("list", { name: "导入步骤" })).toContainText("选择目录");
  await drawer.getByRole("button", { name: /^Games/ }).click();
  await expect(drawer).toContainText("Pegasus BIOS / Games");
  await drawer.getByRole("button", { name: "扫描此目录" }).click();
  const footerClose = drawer.locator("footer").getByRole("button", { name: "关闭", exact: true });
  await expect(footerClose).toBeEnabled();
  await footerClose.click();
  await expect(drawer).toHaveCount(0);

  await page.getByRole("button", { name: /选择目录并扫描|继续扫描或映射/ }).click();
  drawer = page.getByRole("dialog", { name: "从 Pegasus 目录导入游戏" });
  const mapping = drawer.getByRole("combobox", { name: "NES 处理方式" });
  await expect(mapping).toBeVisible({ timeout: 30_000 });
  await expect(mapping).toHaveValue("");
  await expect(drawer.getByRole("button", { name: "确认映射" })).toBeDisabled();
  const nesOption = mapping.getByRole("option", { name: /^导入到 NES 游戏/ });
  await mapping.selectOption(await nesOption.getAttribute("value") ?? "");
  await drawer.getByRole("button", { name: "确认映射" }).click();
  await expect(drawer).toContainText("1 个导入 · 0 个跳过");
  await drawer.getByRole("button", { name: "开始异步导入" }).click();
  await expect(page).toHaveURL(/\/admin\/imports\/server\/pegasus\/[0-9a-f-]+$/);
  await expect(page.getByRole("region", { name: "Pegasus 导入摘要" })).toBeVisible();
  await expect(page.getByText(/^(已完成|部分失败)$/).first()).toBeVisible({ timeout: 60_000 });
  const resultTable = page.getByRole("table", { name: "Pegasus 导入结果" });
  await expect(resultTable).toContainText("Acceptance Game");
  await expect(resultTable).toContainText("视频 READY");
  await page.getByRole("searchbox", { name: "搜索标题" }).fill("Acceptance");
  await page.getByRole("button", { name: "应用筛选" }).click();
  await expect(page).toHaveURL(/q=Acceptance/);
  await expectNoPageOverflow(page);
  await expectNoSeriousAxeViolations(page);
  await page.screenshot({ path: evidencePath(testInfo, "pegasus-import-detail.png"), fullPage: true });
});

test("ACC-MEDIA-001 video upload is explicit in admin and absent from library requests", async ({ page }, testInfo) => {
  test.setTimeout(90_000);
  const gamesResponse = await page.request.get("/api/v1/admin/games?limit=100");
  expect(gamesResponse.ok()).toBe(true);
  const games = await gamesResponse.json() as { items: Array<{ gameId: string; title: string }> };
  const game = games.items.find((item) => item.title === "Sudoku");
  if (!game) throw new Error("acceptance fixture game Sudoku was not found");
  const gameId = game.gameId;

  await page.goto(`/admin/games/${gameId}`);
  const upload = page.locator("#admin-video-upload");
  await expect(upload).toBeEnabled();
  await upload.setInputFiles({
    name: "acceptance.mp4",
    mimeType: "video/mp4",
    buffer: Buffer.from([0, 0, 0, 24, 102, 116, 121, 112, 105, 115, 111, 109, 0, 0, 0, 0, 105, 115, 111, 109, 109, 112, 52, 50]),
  });
  const adminVideo = page.getByLabel(/管理视频预览/);
  await expect(adminVideo).toBeVisible();
  await expect(adminVideo).toHaveAttribute("controls", "");
  await expect(adminVideo).not.toHaveAttribute("autoplay", "");

  const detailResponse = await page.request.get(`/api/v1/games/${gameId}`);
  expect(detailResponse.ok()).toBe(true);
  const detail = await detailResponse.json() as { videoUrl: string | null };
  expect(detail.videoUrl).toBeTruthy();
  let videoRequests = 0;
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === detail.videoUrl) videoRequests += 1;
  });
  await page.goto("/library");
  await expect(page.getByRole("heading", { name: "游戏库" })).toBeVisible();
  expect(videoRequests).toBe(0);

  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto(`/games/${gameId}`);
  const detailVideo = page.getByLabel(/视频预览/);
  await expect(detailVideo).toHaveJSProperty("muted", true);
  await expect(detailVideo).toHaveAttribute("playsinline", "");
  await expect(detailVideo).toHaveAttribute("loop", "");
  await expect(detailVideo).not.toHaveAttribute("autoplay", "");
  await expect(page.getByRole("button", { name: /播放视频预览/ })).toBeVisible();
  await expectNoPageOverflow(page);
  await expectNoSeriousAxeViolations(page);
  await page.screenshot({ path: evidencePath(testInfo, "game-video-reduced-motion.png"), fullPage: true });

  await page.goto(`/admin/games/${gameId}`);
  await page.getByRole("button", { name: "移除" }).click();
  await expect(page.getByText("暂无视频", { exact: true })).toBeVisible();
});

const realBIOSRelativePath = process.env.RETROM_REAL_BIOS_RELATIVE_PATH;

test.describe("authorized real server BIOS source", () => {
  test.skip(!realBIOSRelativePath, "requires an explicitly authorized local BIOS directory");

  test("REAL-BIOS imports authorized files and preserves browser evidence", async ({ page }, testInfo) => {
    test.setTimeout(180_000);
    await page.goto("/admin/imports/server");
    await page.getByRole("button", { name: "选择目录并开始" }).click();
    const drawer = page.getByRole("dialog", { name: "选择 BIOS 所在目录" });
    for (const segment of realBIOSRelativePath!.split("/")) {
      await drawer.getByRole("button", { name: segment, exact: true }).click();
    }
    const escapedRelativePath = realBIOSRelativePath!.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    await expect(drawer.locator("strong").filter({ hasText: new RegExp(`${escapedRelativePath}$`) })).toBeVisible();
    await expect(page.getByRole("checkbox", { name: /允许使用更优候选替换已有 BIOS/ })).not.toBeChecked();
    await drawer.getByRole("button", { name: "开始异步导入" }).click();
    await expect(page).toHaveURL(/\/admin\/imports\/server\/[0-9a-f-]+$/);
    await expect(page.getByText(/^(已完成|部分失败|任务失败)$/).first()).toBeVisible({ timeout: 150_000 });

    const importID = new URL(page.url()).pathname.split("/").at(-1)!;
    const items: Array<{ requirementId: string; logicalName: string; candidateCount: number; selectedRelativePath?: string | null; state: string }> = [];
    let cursor: string | null = null;
    let summary: { state: string; counts: { candidates: number; imported: number; matched: number; warnings: number; failed: number } } | null = null;
    do {
      const query = new URLSearchParams({ limit: "50" });
      if (cursor) query.set("cursor", cursor);
      const response = await page.request.get(`/api/v1/admin/server-imports/${importID}?${query}`);
      expect(response.ok()).toBe(true);
      const payload = await response.json() as { summary: typeof summary; items: typeof items; nextCursor: string | null };
      summary = payload.summary;
      items.push(...payload.items);
      cursor = payload.nextCursor;
    } while (cursor);
    expect(summary).not.toBeNull();
    expect(summary!.counts.candidates).toBeGreaterThan(0);
    expect(items.some((item) => item.candidateCount > 0 && item.selectedRelativePath)).toBe(true);

    await expectNoSeriousAxeViolations(page);
    for (const viewport of [
      { width: 1280, height: 800, name: "1280x800" },
      { width: 2560, height: 1440, name: "2560x1440" },
      { width: 3840, height: 2160, name: "3840x2160" },
    ]) {
      await page.setViewportSize(viewport);
      await expectNoPageOverflow(page);
      await page.screenshot({ path: evidencePath(testInfo, `real-server-bios-${viewport.name}.png`), fullPage: true });
    }
    const candidateButton = page.getByRole("button", { name: /^查看候选（[1-9]/ }).first();
    await candidateButton.click();
    await expect(page.getByRole("alertdialog", { name: /候选排序/ })).toBeVisible();
    await page.screenshot({ path: evidencePath(testInfo, "real-server-bios-candidates.png"), fullPage: true });
    await testInfo.attach("real-server-summary.json", {
      body: Buffer.from(JSON.stringify({ importID, sourceRelativePath: realBIOSRelativePath, summary, candidateItems: items.filter((item) => item.candidateCount > 0) }, null, 2)),
      contentType: "application/json",
    });
  });
});
