import { createHash, randomUUID } from "node:crypto";
import { mkdirSync } from "node:fs";
import path from "node:path";
import axe from "axe-core";
import {
  expect,
  test,
  type Locator,
  type Page,
  type TestInfo,
} from "@playwright/test";

const title = "EmulationStation GBA Smoke";
const romFixture = {
  size: 1_024,
  sha256: "b2e50f15541e172933fd1f0d02355105233f5e36b55d121c07f39079f21347c5",
};
const coverFixture = {
  size: 20_746,
  sha256: "0d72b89ed87fcf349a3422d7f3888183ce57a3fa757bc6baab0365a70f7ccc02",
  mediaType: "image/png",
};
const videoFixture = {
  size: 767,
  sha256: "39a3044ce78c029049bda10b617724203bb91f4e2cb32ec5f15e3bdd45f6d10d",
  mediaType: "video/webm",
};
let csrfToken = "";

type ImportSummary = {
  id: string;
  state: string;
  version: number;
  counts: { reviewPending: number };
};

type ImportItem = {
  executionState: string;
  payloadState: string;
  reviewItemId: string | null;
};

type StorageSnapshot = {
  totals: { unreferencedBytes: string };
  categories: Array<{ code: string; bytes: string; blobCount: number }>;
  details: { cleanupCandidates: { blobCount: number; bytes: string } };
};

test.beforeEach(async ({ page }) => {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000";
  const response = await page.request.post("/api/v1/auth/login", {
    data: { username: "test", password: "test" },
    headers: { Origin: origin },
  });
  expect(response.ok()).toBe(true);
  const payload = await response.json() as { csrfToken: string };
  csrfToken = payload.csrfToken;
});

function evidencePath(testInfo: TestInfo, name: string) {
  const caseDirectory = process.env.RETROM_ACCEPTANCE_CASE_DIR;
  if (!caseDirectory) {
    return testInfo.outputPath(name);
  }
  const screenshots = path.join(caseDirectory, "screenshots");
  mkdirSync(screenshots, { recursive: true });
  return path.join(screenshots, `${testInfo.project.name}-${name}`);
}

async function activateWithKeyboard(control: Locator) {
  await control.focus();
  await expect(control).toBeFocused();
  await control.press("Enter");
}

async function expectNoPageOverflow(page: Page) {
  const fitsViewport = await page.evaluate(
    () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
  );
  expect(fitsViewport).toBe(true);
}

async function expectNoSeriousAxeViolations(page: Page) {
  await page.evaluate(axe.source);
  const violations = await page.evaluate(async () => {
    type AxeResult = {
      violations: Array<{
        id: string;
        impact: string | null;
        nodes: unknown[];
      }>;
    };
    type WindowWithAxe = typeof window & {
      axe: {
        run: (root: Document, options: object) => Promise<AxeResult>;
      };
    };
    const axeAPI = (window as WindowWithAxe).axe;
    const result = await axeAPI.run(document, {
      runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] },
    });
    return result.violations.filter(
      (violation) => violation.impact === "serious" || violation.impact === "critical",
    );
  });
  expect(
    violations,
    `axe serious/critical violations: ${JSON.stringify(violations)}`,
  ).toEqual([]);
}

async function readImport(page: Page, importId: string) {
  const response = await page.request.get(
    `/api/v1/admin/emulationstation-imports/${importId}`,
  );
  expect(response.ok()).toBe(true);
  return await response.json() as ImportSummary;
}

async function readItems(page: Page, importId: string) {
  const response = await page.request.get(
    `/api/v1/admin/emulationstation-imports/${importId}/items?limit=50`,
  );
  expect(response.ok()).toBe(true);
  return (await response.json() as { items: ImportItem[] }).items;
}

async function expectLockedPayload(
  page: Page,
  url: string,
  fixture: { size: number; sha256: string; mediaType: string },
) {
  const response = await page.request.get(url);
  expect(response.ok(), `${url}: ${await response.text()}`).toBe(true);
  expect(response.headers()["content-type"]).toContain(fixture.mediaType);
  const payload = await response.body();
  expect(payload).toHaveLength(fixture.size);
  expect(createHash("sha256").update(payload).digest("hex")).toBe(fixture.sha256);
}

async function expectVideoMetadata(video: Locator) {
  await expect.poll(async () => video.evaluate((element) => {
    const player = element as HTMLVideoElement;
    if (player.error) {
      return `error:${player.error.code}`;
    }
    return `${player.videoWidth}x${player.videoHeight}`;
  }), { timeout: 10_000 }).toBe("160x112");
}

async function readStorageSnapshot(page: Page) {
  const response = await page.request.get("/api/v1/admin/storage-analysis");
  expect(response.ok(), await response.text()).toBe(true);
  return await response.json() as StorageSnapshot;
}

function unreferencedCategory(snapshot: StorageSnapshot) {
  const category = snapshot.categories.find((entry) => entry.code === "UNREFERENCED");
  expect(category).toBeTruthy();
  return category!;
}

async function scanPublicSource(page: Page) {
  await page.goto("/admin/imports/server");
  const capability = page.locator(".emulationstation-capability");
  await expect(
    capability.getByRole("heading", { name: "扫描 gamelist.xml 并准备审核" }),
  ).toBeVisible();
  await activateWithKeyboard(
    capability.getByRole("button", {
      name: /选择目录并扫描|继续扫描或映射/,
    }),
  );
  const drawer = page.getByRole("dialog", {
    name: "从 gamelist.xml 准备审核事项",
  });
  await expect(drawer).toBeVisible();
  await activateWithKeyboard(
    drawer.getByRole("button", { name: /^EmulationStationPlayable$/ }),
  );
  await expect(drawer).toContainText("Pegasus BIOS / EmulationStationPlayable");
  const created = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return url.pathname === "/api/v1/admin/emulationstation-imports"
      && response.request().method() === "POST";
  });
  await activateWithKeyboard(
    drawer.getByRole("button", { name: "扫描此目录" }),
  );
  const plan = await (await created).json() as ImportSummary;
  await expect.poll(
    async () => (await readImport(page, plan.id)).state,
    { timeout: 30_000 },
  ).toBe("AWAITING_MAPPING");
  const mapping = drawer.getByRole("combobox", { name: "根目录 处理方式" });
  await expect(mapping).toBeVisible({ timeout: 30_000 });
  return { drawer, mapping, plan };
}

async function mapToGBA(drawer: Locator, mapping: Locator) {
  await expect(mapping).toHaveValue("");
  const option = mapping.getByRole("option", { name: /^导入到 GBA 游戏/ });
  await mapping.selectOption(await option.getAttribute("value") ?? "");
  await activateWithKeyboard(
    drawer.getByRole("button", { name: "确认映射" }),
  );
  await expect(drawer).toContainText("全部进入待审核，不会自动发布");
}

async function verifyResponsiveImportExperience(page: Page, testInfo: TestInfo) {
  const projectName = testInfo.project.name;
  if (projectName === "chrome-1280") {
    await page.setViewportSize({ width: 390, height: 844 });
  }

  const { drawer, mapping, plan } = await scanPublicSource(page);
  const steps = drawer.getByRole("list", { name: "导入步骤" });
  await expect(steps).toContainText("选择目录");
  await expect(steps).toContainText("检查与映射");
  await expect(steps).toContainText("确认审核计划");
  await expect(drawer).toContainText("1 个游戏");
  await expectNoPageOverflow(page);
  await mapToGBA(drawer, mapping);
  await expectNoSeriousAxeViolations(page);
  const mappingEvidenceName = projectName === "chrome-1280"
    ? "emulationstation-mobile-mapping.png"
    : "emulationstation-desktop-mapping.png";
  await page.screenshot({
    path: evidencePath(testInfo, mappingEvidenceName),
    fullPage: true,
  });

  if (projectName === "chrome-1280") {
    await page.setViewportSize({ width: 1280, height: 800 });
    await expectNoPageOverflow(page);
    await expectNoSeriousAxeViolations(page);
    await page.screenshot({
      path: evidencePath(testInfo, "emulationstation-1280-mapping.png"),
      fullPage: true,
    });
  } else if (projectName === "chrome-4k-150") {
    expect(await page.evaluate(() => window.devicePixelRatio)).toBe(1.5);
  }

  await activateWithKeyboard(
    drawer
      .locator("footer")
      .getByRole("button", { name: "关闭", exact: true }),
  );
  await page.goto(`/admin/imports/server/emulationstation/${plan.id}`);
  await expect(
    page.getByRole("region", { name: "EmulationStation 导入摘要" }),
  ).toBeVisible();
  await activateWithKeyboard(
    page.getByRole("button", { name: "继续映射" }),
  );
  const recovered = page.getByRole("dialog", {
    name: "从 gamelist.xml 准备审核事项",
  });
  await expect(recovered).toContainText("全部进入待审核，不会自动发布");
  await activateWithKeyboard(
    recovered
      .locator("footer")
      .getByRole("button", { name: "关闭", exact: true }),
  );
  await expectNoPageOverflow(page);
  await expectNoSeriousAxeViolations(page);
  await page.screenshot({
    path: evidencePath(testInfo, "emulationstation-detail.png"),
    fullPage: true,
  });
  await activateWithKeyboard(
    page.getByRole("button", { name: "删除计划" }),
  );
  const deletion = page.getByRole("alertdialog", {
    name: "删除这份未执行计划？",
  });
  await activateWithKeyboard(
    deletion.getByRole("button", { name: "删除计划" }),
  );
  await expect(page).toHaveURL(/\/admin\/imports\/server$/);
  const deleted = await page.request.get(
    `/api/v1/admin/emulationstation-imports/${plan.id}`,
  );
  expect(deleted.status()).toBe(404);
}

async function verifyFullProductLifecycle(page: Page, testInfo: TestInfo) {
  await page.addInitScript(() => {
    Object.defineProperty(Element.prototype, "requestFullscreen", {
      configurable: true,
      value: () => Promise.resolve(),
    });
  });

  const before = await page.request.get(
    `/api/v1/games?q=${encodeURIComponent(title)}&limit=100`,
  );
  expect(before.ok()).toBe(true);
  expect((await before.json() as { items: unknown[] }).items).toHaveLength(0);

  const { drawer, mapping, plan } = await scanPublicSource(page);
  await mapToGBA(drawer, mapping);
  await drawer.getByRole("button", { name: "开始准备审核事项" }).click();
  await expect(page).toHaveURL(
    new RegExp(`/admin/imports/server/emulationstation/${plan.id}$`),
  );
  await expect.poll(
    async () => (await readImport(page, plan.id)).state,
    { timeout: 60_000 },
  ).toBe("COMPLETED");
  const resultTable = page.getByRole("table", { name: "EmulationStation 导入结果" });
  await expect(
    resultTable.getByRole("row").filter({ hasText: title }),
  ).toContainText("待管理员审核");
  const reviewLink = page.getByRole("link", { name: /逐项审核 1 个游戏/ });
  await expect(reviewLink).toHaveAttribute(
    "href",
    `/admin/reviews?emulationStationImportId=${plan.id}`,
  );
  const reviewAPI = await page.request.get(
    `/api/v1/admin/reviews?emulationStationImportId=${plan.id}&limit=20`,
  );
  expect(reviewAPI.ok(), await reviewAPI.text()).toBe(true);
  const reviewList = await reviewAPI.json() as {
    items: Array<{ itemId: string }>;
  };
  expect(reviewList.items).toHaveLength(1);
  const reviewDetailAPI = await page.request.get(
    `/api/v1/admin/reviews/${reviewList.items[0].itemId}`,
  );
  expect(reviewDetailAPI.ok(), await reviewDetailAPI.text()).toBe(true);
  const reviewDetail = await reviewDetailAPI.json() as {
    sourceMedia: {
      coverUrl: string | null;
      videoUrl: string | null;
      coverWidthPx: number | null;
      coverHeightPx: number | null;
    };
  };
  expect(reviewDetail.sourceMedia.coverWidthPx).toBe(70);
  expect(reviewDetail.sourceMedia.coverHeightPx).toBe(98);
  expect(reviewDetail.sourceMedia.coverUrl).toBeTruthy();
  expect(reviewDetail.sourceMedia.videoUrl).toBeTruthy();
  await expectLockedPayload(page, reviewDetail.sourceMedia.coverUrl!, coverFixture);
  await expectLockedPayload(page, reviewDetail.sourceMedia.videoUrl!, videoFixture);
  await reviewLink.click();
  await expect(page).toHaveURL(
    new RegExp(`/admin/reviews\\?emulationStationImportId=${plan.id}$`),
  );

  const reviewRow = page.locator(".review-workflow-row").filter({ hasText: title });
  await expect(reviewRow).toContainText("可以发布", { timeout: 30_000 });
  await reviewRow.getByRole("link", { name: "审核条目" }).click();
  await expect(page.getByText(/来源：EmulationStation/)).toBeVisible();
  const reviewCover = page.getByRole("img", { name: "当前选择的游戏封面" });
  await expect(reviewCover).toBeVisible();
  await expect(reviewCover).toHaveAttribute("src", reviewDetail.sourceMedia.coverUrl!);
  const reviewVideo = page.locator(".review-source-video video");
  await expect(reviewVideo).toBeVisible();
  await expect(reviewVideo).toHaveAttribute("src", reviewDetail.sourceMedia.videoUrl!);
  await expectVideoMetadata(reviewVideo);
  await page.getByRole("button", { name: "通过并发布" }).click();
  await expect(page.locator(".app-toast")).toContainText("游戏已成功发布", {
    timeout: 20_000,
  });

  let gameId = "";
  await expect.poll(async () => {
    const response = await page.request.get(
      `/api/v1/admin/games?q=${encodeURIComponent(title)}&limit=100`,
    );
    const payload = await response.json() as {
      items: Array<{ gameId: string; title: string }>;
    };
    gameId = payload.items.find((game) => game.title === title)?.gameId ?? "";
    return payload.items.filter((game) => game.title === title).length;
  }, { timeout: 20_000 }).toBe(1);
  await expect.poll(
    async () => (await readItems(page, plan.id))[0]?.payloadState,
    { timeout: 30_000 },
  ).toBe("RELEASED");

  await page.goto(`/games/${gameId}`);
  const publicDetailAPI = await page.request.get(`/api/v1/games/${gameId}`);
  expect(publicDetailAPI.ok(), await publicDetailAPI.text()).toBe(true);
  const publicDetail = await publicDetailAPI.json() as {
    coverUrl: string | null;
    videoUrl: string | null;
  };
  expect(publicDetail.coverUrl).toBeTruthy();
  expect(publicDetail.videoUrl).toBeTruthy();
  await expectLockedPayload(page, publicDetail.coverUrl!, coverFixture);
  await expectLockedPayload(page, publicDetail.videoUrl!, videoFixture);
  const gameCover = page.getByRole("img", { name: `${title} 封面` });
  await expect(gameCover).toBeVisible();
  await expect(gameCover).toHaveAttribute("src", publicDetail.coverUrl!);
  const gameVideo = page.getByLabel(`${title} 视频预览`);
  await expect(gameVideo).toBeVisible();
  await expect(gameVideo).toHaveAttribute("src", publicDetail.videoUrl!);
  await expectVideoMetadata(gameVideo);
  await page.getByRole("button", { name: "播放视频预览" }).click();
  await expect(page.getByText("正在循环播放视频预览")).toBeVisible();
  await expect.poll(
    async () => gameVideo.evaluate((element) => (element as HTMLVideoElement).currentTime),
    { timeout: 10_000 },
  ).toBeGreaterThan(0);
  const configResponse = page.waitForResponse(
    (response) => /\/runtime\/launches\/[^/]+\/config$/.test(response.url())
      && response.status() === 200,
  );
  await page.getByRole("button", { name: "开始游戏" }).click();
  await expect(page).toHaveURL(/\/play\/[0-9a-f-]+$/, { timeout: 10_000 });
  const configHTTP = await configResponse;
  const configURL = configHTTP.url();
  const config = await configHTTP.json() as {
    gameTitle: string;
    core: string;
    coreName: string;
    playerAdapterId: string;
    emulatorjsVersion: string;
    gameUrl: string;
  };
  expect(config).toMatchObject({
    gameTitle: title,
    core: "mgba",
    coreName: "mGBA",
    playerAdapterId: "ejs-4.2.3-v3",
    emulatorjsVersion: "4.2.3",
  });
  expect(config.gameUrl).toMatch(
    /\/runtime\/launches\/[0-9a-f-]+\/game\/emulationstation-smoke\.gba$/,
  );
  const content = await page.request.get(config.gameUrl);
  expect(content.ok()).toBe(true);
  const contentBody = await content.body();
  const contentHash = createHash("sha256")
    .update(contentBody)
    .digest("hex");
  expect(contentBody).toHaveLength(romFixture.size);
  expect(contentHash).toBe(romFixture.sha256);

  const player = page.frameLocator('iframe[title="Retrom EmulatorJS Player"]');
  await expect(player.locator("canvas.ejs_canvas")).toBeVisible({ timeout: 60_000 });
  const frame = page.frames().find((candidate) => candidate !== page.mainFrame());
  expect(frame).toBeTruthy();
  const initialFrame = await frame!.evaluate(
    () => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0,
  );
  await expect.poll(
    async () => frame!.evaluate(
      () => window.EJS_emulator?.gameManager?.getFrameNum?.() ?? 0,
    ),
    { timeout: 30_000 },
  ).toBeGreaterThan(initialFrame + 30);
  await page.screenshot({
    path: evidencePath(testInfo, "emulationstation-gba-player-running.png"),
    fullPage: true,
  });

  const adminDetail = await page.request.get(`/api/v1/admin/games/${gameId}`);
  const adminDetailBody = await adminDetail.text();
  expect(adminDetail.ok(), adminDetailBody).toBe(true);
  const adminGame = JSON.parse(adminDetailBody) as {
    deleteImpact: { sourceKinds: string[] };
  };
  expect(adminGame.deleteImpact.sourceKinds).toContain("SERVER_SCAN");
  const storageBeforeDelete = await readStorageSnapshot(page);
  await page.goto(`/admin/games/${gameId}`);
  await expect(
    page.getByRole("heading", { level: 1, name: title }),
  ).toBeVisible();
  await page.getByRole("button", { name: "永久删除游戏" }).click();
  const deletion = page.getByRole("alertdialog", { name: `永久删除“${title}”？` });
  await expect(deletion.getByRole("textbox")).toHaveCount(0);
  await deletion.getByRole("button", { name: "永久删除游戏" }).click();
  await expect(page.locator(".app-toast")).toContainText("游戏已删除", {
    timeout: 20_000,
  });
  await expect.poll(async () => {
    const response = await page.request.get(`/api/v1/admin/games/${gameId}`);
    if (!response.ok()) {
      return "";
    }
    const game = await response.json() as { status: string; payloadState: string };
    return `${game.status}:${game.payloadState}`;
  }, { timeout: 60_000 }).toBe("DELETED:RELEASED");

  const releasedBytes = BigInt(
    romFixture.size + coverFixture.size + videoFixture.size,
  );
  const beforeUnreferenced = unreferencedCategory(storageBeforeDelete);
  await expect.poll(async () => {
    const after = await readStorageSnapshot(page);
    const afterUnreferenced = unreferencedCategory(after);
    return {
      totalBytes: BigInt(after.totals.unreferencedBytes)
        - BigInt(storageBeforeDelete.totals.unreferencedBytes),
      categoryBytes: BigInt(afterUnreferenced.bytes)
        - BigInt(beforeUnreferenced.bytes),
      categoryCount: afterUnreferenced.blobCount - beforeUnreferenced.blobCount,
      candidateBytes: BigInt(after.details.cleanupCandidates.bytes)
        - BigInt(storageBeforeDelete.details.cleanupCandidates.bytes),
      candidateCount: after.details.cleanupCandidates.blobCount
        - storageBeforeDelete.details.cleanupCandidates.blobCount,
    };
  }, { timeout: 60_000 }).toEqual({
    totalBytes: releasedBytes,
    categoryBytes: releasedBytes,
    categoryCount: 3,
    candidateBytes: releasedBytes,
    candidateCount: 3,
  });

  expect((await page.request.get(`/api/v1/games/${gameId}`)).ok()).toBe(false);
  expect((await page.request.get(configURL)).ok()).toBe(false);
  expect((await page.request.get(config.gameUrl)).ok()).toBe(false);
  expect((await page.request.get(publicDetail.coverUrl!)).ok()).toBe(false);
  expect((await page.request.get(publicDetail.videoUrl!)).ok()).toBe(false);
  const launch = await page.request.post("/api/v1/launches", {
    data: {
      gameId,
      coreId: null,
      saveStateId: null,
      dosEntry: null,
      returnTo: `/games/${gameId}`,
      clientCapabilities: {
        secureContext: true,
        crossOriginIsolated: true,
        sharedArrayBuffer: true,
      },
    },
    headers: {
      Origin: process.env.RETROM_WEB_ORIGIN ?? "http://localhost:3000",
      "X-Retrom-Csrf": csrfToken,
      "Idempotency-Key": randomUUID(),
    },
  });
  expect(launch.ok()).toBe(false);
  const after = await page.request.get(
    `/api/v1/games?q=${encodeURIComponent(title)}&limit=100`,
  );
  expect((await after.json() as { items: unknown[] }).items).toHaveLength(0);
}

test(
  "ACC-ES-005 EmulationStation three-card drawer, explicit mapping and responsive recovery",
  async ({ page }, testInfo) => {
    test.setTimeout(120_000);
    await verifyResponsiveImportExperience(page, testInfo);
  },
);

test(
  "ACC-ES-006 project-owned EmulationStation GBA publishes, runs and releases after deletion",
  async ({ page }, testInfo) => {
    test.skip(
      testInfo.project.name !== "chrome-1280",
      "全链产品场景只需在 chrome-1280 执行一次",
    );
    test.setTimeout(240_000);
    await verifyFullProductLifecycle(page, testInfo);
  },
);
