import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import path from "node:path";
import { expect, test, type APIRequestContext, type BrowserContext, type Locator, type Page, type TestInfo } from "@playwright/test";
import axe from "axe-core";

const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
const userPassword = "a sufficiently long favorite password";

type AuthContext = { csrfToken: string };

function evidencePath(testInfo: TestInfo, name: string) {
  const caseDirectory = process.env.RETROM_ACCEPTANCE_CASE_DIR;
  if (!caseDirectory) {return testInfo.outputPath(name);}
  const screenshots = path.join(caseDirectory, "screenshots");
  mkdirSync(screenshots, { recursive: true });
  return path.join(screenshots, `${testInfo.project.name}-${name}`);
}

async function login(request: APIRequestContext, username = "test", password = "test") {
  const response = await request.post("/api/v1/auth/login", { data: { username, password }, headers: { Origin: origin } });
  expect(response.ok()).toBe(true);
  return response.json() as Promise<AuthContext>;
}

async function createOtherUser(request: APIRequestContext, context: BrowserContext, csrfToken: string) {
  const invitation = await request.post("/api/v1/admin/invitations", {
    data: { role: "USER", confirmAdminRole: false },
    headers: { Origin: origin, "X-Retrom-Csrf": csrfToken, "Idempotency-Key": crypto.randomUUID() },
  });
  expect(invitation.status()).toBe(201);
  const token = new URL((await invitation.json() as { url: string }).url).hash.replace("#invite=", "");
  const accepted = await context.request.post("/api/v1/auth/invitations/accept", {
    data: { token, username: "favorite-other", displayName: "Favorite Other", password: userPassword, passwordConfirmation: userPassword },
    headers: { Origin: origin },
  });
  expect(accepted.status()).toBe(201);
}

async function noPageOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true);
}

async function favoriteHeartVisual(heart: Locator) {
  return heart.evaluate((element) => {
    const button = element.getBoundingClientRect();
    const icon = element.querySelector("svg")!.getBoundingClientRect();
    const iconStyle = getComputedStyle(element.querySelector("svg")!);
    return {
      buttonWidth: button.width,
      buttonHeight: button.height,
      iconWidth: icon.width,
      iconHeight: icon.height,
      centerOffsetX: icon.left + icon.width / 2 - (button.left + button.width / 2),
      centerOffsetY: icon.top + icon.height / 2 - (button.top + button.height / 2),
      color: getComputedStyle(element).color,
      fill: iconStyle.fill,
    };
  });
}

test("ACC-FAV-003 user flow remains consistent across library, detail, folders, batch and owner", async ({ page, browser }, testInfo) => {
  test.setTimeout(120_000);
  page.setDefaultTimeout(10_000);
  test.skip(testInfo.project.name !== "chrome-1280", "The stateful favorite flow runs once.");
  const admin = await login(page.request);
  await page.goto("/library?platformId=gba");

  const available = page.locator('.library-game-card:has(button[aria-label^="收藏“"])');
  expect(await available.count()).toBeGreaterThanOrEqual(2);
  const firstId = await available.nth(0).getAttribute("data-library-game");
  const secondId = await available.nth(1).getAttribute("data-library-game");
  const first = page.locator(`[data-library-game="${firstId}"]`);
  const second = page.locator(`[data-library-game="${secondId}"]`);
  const firstTitle = await first.locator("h2").innerText();
  const emptyHeart = first.getByRole("button", { name: `收藏“${firstTitle}”` });
  const emptyHeartVisual = await favoriteHeartVisual(emptyHeart);
  await emptyHeart.click();
  const filledHeart = first.getByRole("button", { name: `取消收藏“${firstTitle}”` });
  await expect(filledHeart).toHaveAttribute("aria-pressed", "true");
  const filledHeartVisual = await favoriteHeartVisual(filledHeart);
  expect(emptyHeartVisual).toMatchObject({ fill: "none" });
  expect(filledHeartVisual).toMatchObject({ color: "rgb(220, 66, 87)", fill: "rgb(220, 66, 87)" });
  for (const visual of [emptyHeartVisual, filledHeartVisual]) {
    expect(visual.buttonWidth).toBeCloseTo(38, 3);
    expect(visual.buttonHeight).toBeCloseTo(38, 3);
    expect(visual.iconWidth).toBeCloseTo(18, 3);
    expect(visual.iconHeight).toBeCloseTo(18, 3);
  }
  expect(Math.abs(emptyHeartVisual.centerOffsetX)).toBeLessThanOrEqual(0.5);
  expect(Math.abs(emptyHeartVisual.centerOffsetY)).toBeLessThanOrEqual(0.5);
  expect(Math.abs(filledHeartVisual.centerOffsetX)).toBeLessThanOrEqual(0.5);
  expect(Math.abs(filledHeartVisual.centerOffsetY)).toBeLessThanOrEqual(0.5);
  await second.getByRole("button", { name: /^收藏“/ }).click();

  const firstMore = first.getByRole("button", { name: `游戏“${firstTitle}”的更多操作` });
  await firstMore.click();
  await first.getByRole("menuitem", { name: "管理收藏夹" }).click();
  const firstPicker = page.getByRole("dialog", { name: new RegExp(`管理“${firstTitle}”的收藏夹`) });
  await expect(firstPicker).toBeVisible();
  await expect(firstPicker.getByRole("searchbox", { name: "搜索收藏夹" })).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(firstMore).toBeFocused();
  await firstMore.click();
  await first.getByRole("menuitem", { name: "管理收藏夹" }).click();
  await page.getByRole("button", { name: "＋ 新建收藏夹" }).click();
  let nameDialog = page.getByRole("dialog", { name: "新建收藏夹" });
  await nameDialog.getByRole("textbox", { name: "收藏夹名称" }).fill("待通关");
  await nameDialog.getByRole("button", { name: "创建收藏夹" }).click();
  await expect(page.getByRole("checkbox", { name: /待通关/ })).toBeChecked();
  await page.getByRole("button", { name: "＋ 新建收藏夹" }).click();
  nameDialog = page.getByRole("dialog", { name: "新建收藏夹" });
  await nameDialog.getByRole("textbox", { name: "收藏夹名称" }).fill("RPG");
  await nameDialog.getByRole("button", { name: "创建收藏夹" }).click();
  await expect(page.getByRole("checkbox", { name: /待通关/ })).toBeChecked();
  await expect(page.getByRole("checkbox", { name: /RPG/ })).toBeChecked();
  await page.getByRole("dialog", { name: new RegExp(`管理“${firstTitle}”的收藏夹`) }).getByRole("button", { name: "完成" }).click();

  await first.getByRole("link").first().click();
  await expect(page.getByRole("button", { name: `取消收藏“${firstTitle}”` })).toHaveAttribute("aria-pressed", "true");
  await expect(page.getByRole("button", { name: "开始游戏" })).toBeVisible();

  await page.getByRole("navigation", { name: "主要导航" }).getByRole("link", { name: "我的收藏" }).click();
  await expect(page).toHaveURL(/\/favorites$/);
  await expect(page.getByRole("heading", { name: "我的收藏" })).toBeVisible();
  await page.getByRole("searchbox", { name: "搜索收藏" }).fill(firstTitle);
  await expect(page.getByRole("heading", { name: firstTitle })).toBeVisible();
  const filteredURL = page.url();
  await page.getByRole("heading", { name: firstTitle }).click();
  await expect(page).toHaveURL(new RegExp(`/games/${firstId}$`));
  await page.goBack();
  await expect(page).toHaveURL(filteredURL);
  await expect(page.getByRole("heading", { name: firstTitle })).toBeVisible();
  await page.getByRole("searchbox", { name: "搜索收藏" }).fill("");
  await page.getByRole("combobox", { name: "排序方式" }).selectOption("TITLE_ASC");
  await expect(page).toHaveURL(/sort=TITLE_ASC/);
  await page.getByRole("button", { name: /Game Boy Advance 2$/ }).click();
  await expect(page).toHaveURL(/platformId=gba/);
  await page.getByRole("button", { name: /^全部 \d+$/ }).click();
  await expect(page).not.toHaveURL(/platformId=/);
  await page.getByRole("button", { name: /待通关 1$/ }).click();
  await page.getByRole("button", { name: "编辑收藏夹" }).click();
  const rename = page.getByRole("dialog", { name: "编辑收藏夹" });
  await rename.getByRole("textbox", { name: "收藏夹名称" }).fill("近期必玩");
  await rename.getByRole("button", { name: "保存" }).click();
  await expect(page.getByRole("heading", { name: "近期必玩" })).toBeVisible();

  await page.getByRole("button", { name: /全部收藏 \d+$/ }).click();
  await page.getByRole("button", { name: "批量整理" }).click();
  const selectors = page.locator(".favorite-select");
  await selectors.nth(0).click();
  await selectors.nth(1).click();
  await page.getByRole("button", { name: "加入收藏夹" }).click();
  const batchPicker = page.getByRole("dialog", { name: /将 2 款游戏加入收藏夹/ });
  await batchPicker.getByRole("checkbox", { name: /RPG/ }).check();
  await batchPicker.getByRole("button", { name: "完成" }).click();

  await page.getByRole("button", { name: /RPG 2$/ }).click();
  await page.getByRole("button", { name: "批量整理" }).click();
  await page.locator(".favorite-select").first().click();
  await page.getByRole("button", { name: "从当前收藏夹移除" }).click();
  await expect(page.getByRole("button", { name: /RPG 1$/ })).toBeVisible();

  await page.getByRole("button", { name: /近期必玩 1$/ }).click();
  await page.getByRole("button", { name: "编辑收藏夹" }).click();
  await page.getByRole("dialog", { name: "编辑收藏夹" }).getByRole("button", { name: "删除收藏夹…" }).click();
  await page.getByRole("alertdialog", { name: /删除“近期必玩”/ }).getByRole("button", { name: "删除收藏夹" }).click();
  await expect(page.getByRole("button", { name: /近期必玩 \d+$/ })).toHaveCount(0);

  await page.getByRole("button", { name: "批量整理" }).click();
  await page.locator(".favorite-select").first().click();
  await page.getByRole("button", { name: "取消收藏", exact: true }).click();
  await page.getByRole("alertdialog", { name: /取消收藏 1 款游戏/ }).getByRole("button", { name: "取消收藏", exact: true }).click();
  await page.getByRole("button", { name: "撤销" }).click();
  await expect(page.getByText("已恢复收藏", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.locator(".favorite-game-card")).toHaveCount(2);

  const otherContext = await browser.newContext({ baseURL: origin });
  await createOtherUser(page.request, otherContext, admin.csrfToken);
  const otherPage = await otherContext.newPage();
  await otherPage.goto("/favorites");
  await expect(otherPage.getByRole("heading", { name: "还没有收藏游戏" })).toBeVisible();
  await expect(otherPage.getByText("待通关")).toHaveCount(0);
  await expect(otherPage.getByText("RPG", { exact: true })).toHaveCount(0);
  await otherContext.close();
  await page.screenshot({ path: evidencePath(testInfo, "favorite-user-flow.png"), fullPage: true });
});

test("ACC-FAV-004 favorite states, keyboard semantics and bounded layout hold at every desktop viewport", async ({ page }, testInfo) => {
  test.setTimeout(150_000);
  page.setDefaultTimeout(10_000);
  await login(page.request);

  if (testInfo.project.name === "chrome-1280") {
    await page.goto("/favorites");
    const create = page.getByRole("button", { name: "＋ 新建收藏夹" });
    await create.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("textbox", { name: "收藏夹名称" })).toBeFocused();
    await page.keyboard.type("键盘收藏夹");
    await page.keyboard.press("Enter");
    await expect(page.getByRole("button", { name: /键盘收藏夹/ })).toBeVisible();
  }

  const database = process.env.RETROM_E2E_DATABASE;
  expect(database, "RETROM_E2E_DATABASE must point to the temporary acceptance database").toBeTruthy();
  execFileSync(path.resolve("../scripts/acceptance/seed-favorites-layout.sh"), [database!], { stdio: "pipe" });
  await page.goto("/favorites");
  await expect(page.locator(".favorite-game-card")).toHaveCount(50);
  await expect(page.locator(".favorite-game-card").first()).toBeVisible();
  await noPageOverflow(page);

  const layout = await page.evaluate(() => {
    const rail = document.querySelector<HTMLElement>(".favorite-rail")!;
    const railList = document.querySelector<HTMLElement>(".favorite-rail nav")!;
    const cards = [...document.querySelectorAll<HTMLElement>(".favorite-game-card")];
    const firstCard = cards[0].getBoundingClientRect();
    const firstCover = cards[0].querySelector<HTMLElement>(".favorite-game-cover")!.getBoundingClientRect();
    const heart = cards[0].querySelector<HTMLElement>(".favorite-heart")!;
    const heartRect = heart.getBoundingClientRect();
    const heartIcon = heart.querySelector("svg")!;
    const heartIconRect = heartIcon.getBoundingClientRect();
    const manage = cards[0].querySelector<HTMLElement>(".favorite-manage")!;
    const manageRect = manage.getBoundingClientRect();
    const toolbarControl = document.querySelector<HTMLElement>(".favorite-toolbar select")!;
    return {
      cardWidths: cards.map((card) => card.getBoundingClientRect().width),
      railListScrollable: railList.scrollHeight > railList.clientHeight,
      railOverflow: getComputedStyle(rail).overflow,
      railHeight: rail.clientHeight,
      viewportHeight: document.documentElement.clientHeight,
      controlHeight: toolbarControl.getBoundingClientRect().height,
      helperFont: Number.parseFloat(getComputedStyle(document.querySelector<HTMLElement>(".favorite-head-summary")!).fontSize),
      railLabelFont: Number.parseFloat(getComputedStyle(document.querySelector<HTMLElement>(".favorite-rail-label")!).fontSize),
      cardTitleFont: Number.parseFloat(getComputedStyle(document.querySelector<HTMLElement>(".favorite-game-body h3")!).fontSize),
      heartWidth: heartRect.width,
      heartHeight: heartRect.height,
      heartIconWidth: heartIconRect.width,
      heartIconHeight: heartIconRect.height,
      heartCenterOffsetX: heartIconRect.left + heartIconRect.width / 2 - (heartRect.left + heartRect.width / 2),
      heartCenterOffsetY: heartIconRect.top + heartIconRect.height / 2 - (heartRect.top + heartRect.height / 2),
      heartColor: getComputedStyle(heart).color,
      heartFill: getComputedStyle(heartIcon).fill,
      heartRadius: getComputedStyle(heart).borderRadius,
      manageText: manage.textContent,
      manageInsideBody: manageRect.top >= firstCover.bottom && manageRect.bottom <= firstCard.bottom + 1,
      toolbarBackground: getComputedStyle(document.querySelector<HTMLElement>(".favorite-toolbar")!).backgroundColor,
      summaryBackground: getComputedStyle(document.querySelector<HTMLElement>(".favorite-platforms")!).backgroundColor,
    };
  });
  expect(Math.min(...layout.cardWidths)).toBeGreaterThanOrEqual(270);
  expect(Math.max(...layout.cardWidths)).toBeLessThanOrEqual(320);
  expect(layout.railListScrollable).toBe(true);
  expect(layout.railOverflow).toBe("hidden");
  expect(layout.railHeight).toBeLessThan(layout.viewportHeight);
  expect({ width: layout.heartWidth, height: layout.heartHeight }).toEqual({ width: 38, height: 38 });
  expect({ width: layout.heartIconWidth, height: layout.heartIconHeight }).toEqual({ width: 18, height: 18 });
  expect(Math.abs(layout.heartCenterOffsetX)).toBeLessThanOrEqual(0.5);
  expect(Math.abs(layout.heartCenterOffsetY)).toBeLessThanOrEqual(0.5);
  expect(layout.heartColor).toBe("rgb(220, 66, 87)");
  expect(layout.heartFill).toBe("rgb(220, 66, 87)");
  expect(layout.heartRadius).toBe("50%");
  expect(layout.manageText).toBe("•••");
  expect(layout.manageInsideBody).toBe(true);
  expect(layout.toolbarBackground).toBe("rgb(255, 255, 255)");
  expect(layout.summaryBackground).toBe("rgba(0, 0, 0, 0)");
  await expect(page.getByRole("button", { name: "＋ 新建收藏夹" })).toBeVisible();
  if (testInfo.project.name === "chrome-4k-150") {
    expect(layout.controlHeight).toBeGreaterThanOrEqual(42);
    expect(layout.helperFont).toBeGreaterThanOrEqual(12);
    expect(layout.railLabelFont).toBeGreaterThanOrEqual(12);
    expect(layout.cardTitleFont).toBeGreaterThanOrEqual(16);
  }

  if (testInfo.project.name === "chrome-1280") {
    await page.getByRole("button", { name: "批量整理" }).click();
    const selectionButtons = page.locator(".favorite-select");
    for (let index = 0; index < 50; index += 1) {await selectionButtons.nth(index).click();}
    await expect(page.locator(".favorite-batch")).toContainText("已选择 50 款");
    const batchLayout = await page.evaluate(() => {
      const batch = document.querySelector<HTMLElement>(".favorite-batch")!.getBoundingClientRect();
      const last = [...document.querySelectorAll<HTMLElement>(".favorite-game-card")].at(-1)!.getBoundingClientRect();
      return { batchWidth: batch.width, lastBottom: last.bottom, viewportHeight: innerHeight, pagePaddingBottom: Number.parseFloat(getComputedStyle(document.querySelector<HTMLElement>(".favorite-page")!).paddingBottom) };
    });
    expect(batchLayout.batchWidth).toBeLessThanOrEqual(720);
    expect(batchLayout.pagePaddingBottom).toBeGreaterThanOrEqual(80);
    await page.getByRole("button", { name: "取消选择", exact: true }).click();

    const pointerHeart = page.locator(".favorite-heart").first();
    await pointerHeart.hover();
    await pointerHeart.click();
    const pointerDialog = page.getByRole("alertdialog", { name: /取消收藏/ });
    const pointerBackdrop = page.locator("body > .dialog-backdrop", { has: pointerDialog });
    await expect(pointerBackdrop).toBeVisible();
    const viewport = page.viewportSize()!;
    const beforeMove = await pointerDialog.boundingBox();
    const backdropBox = await pointerBackdrop.boundingBox();
    expect(backdropBox).toEqual({ x: 0, y: 0, width: viewport.width, height: viewport.height });
    expect(beforeMove?.width).toBeGreaterThan(500);
    await page.mouse.move(1, 1);
    await page.mouse.move(viewport.width - 1, viewport.height - 1);
    const afterMove = await pointerDialog.boundingBox();
    expect(afterMove).toEqual(beforeMove);
    await page.keyboard.press("Escape");
    await expect(pointerHeart).toBeFocused();

    const manage = page.locator(".favorite-manage").first();
    await manage.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("dialog", { name: /管理.*收藏夹/ })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(manage).toBeFocused();
    const heart = page.locator(".favorite-heart").first();
    await heart.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("alertdialog", { name: /取消收藏/ })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(heart).toBeFocused();
  }

  await page.emulateMedia({ reducedMotion: "reduce" });
  expect(await page.locator(".favorite-game-card").first().evaluate((element) => Number.parseFloat(getComputedStyle(element).transitionDuration))).toBeLessThanOrEqual(0.01);

  await page.getByRole("searchbox", { name: "搜索收藏" }).fill("definitely missing");
  await expect(page.getByRole("heading", { name: "没有匹配的收藏" })).toBeVisible();
  await page.getByRole("button", { name: "清除筛选" }).click();
  await page.getByRole("button", { name: /布局收藏夹 001 0$/ }).click();
  await expect(page.getByRole("heading", { name: "此收藏夹还没有游戏" })).toBeVisible();
  if (testInfo.project.name === "chrome-1280") {
    const folderId = "73000000-0000-7000-8000-000000000001";
    await page.route(`**/api/v1/favorite-folders/${folderId}`, async (route) => {
      if (route.request().method() !== "PATCH") {return route.continue();}
      await route.fulfill({
        status: 412,
        contentType: "application/json",
        body: JSON.stringify({ error: { code: "RESOURCE_VERSION_CONFLICT", message: "收藏夹已被修改" } }),
      });
    });
    await page.getByRole("button", { name: "编辑收藏夹" }).click();
    const conflict = page.getByRole("dialog", { name: "编辑收藏夹" });
    await conflict.getByRole("textbox", { name: "收藏夹名称" }).fill("保留的冲突名称");
    await conflict.getByRole("button", { name: "保存" }).click();
    await expect(conflict.getByRole("alert")).toContainText("已刷新真实版本");
    await expect(conflict.getByRole("textbox", { name: "收藏夹名称" })).toHaveValue("保留的冲突名称");
    await conflict.getByRole("button", { name: "取消" }).click();
    await page.unroute(`**/api/v1/favorite-folders/${folderId}`);
  }
  await page.evaluate(axe.source);
  const violations = await page.evaluate(async () => {
    const axeAPI = (window as typeof window & { axe: { run: (root: Document, options: object) => Promise<{ violations: Array<{ id: string; impact: string | null }> }> } }).axe;
    const result = await axeAPI.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
    return result.violations.filter((violation) => violation.impact === "serious" || violation.impact === "critical");
  });
  expect(violations).toEqual([]);
  await page.getByRole("button", { name: /^全部收藏 \d+$/ }).click();
  await expect(page.locator(".favorite-game-card")).toHaveCount(50);
  await page.screenshot({ path: evidencePath(testInfo, "favorite-layout.png") });
  await page.goto("/favorites?scope=FOLDER&folderId=79900000-0000-7000-8000-000000000001");
  await expect(page.locator(".favorite-error[role='alert']")).toContainText("收藏夹不存在");
  await expect(page.getByRole("button", { name: "返回全部收藏" })).toBeVisible();
});
