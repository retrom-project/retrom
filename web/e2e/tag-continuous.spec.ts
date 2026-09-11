import { expect, test } from "@playwright/test";

test("ACC-TAG-005 repeated selection and creation keep tag controls open", async ({ page }, testInfo) => {
  const origin = process.env.RETROM_WEB_ORIGIN ?? "http://localhost:4000";
  expect((await page.request.post("/api/v1/auth/login", { headers: { Origin: origin }, data: { username: "test", password: "test" } })).ok()).toBe(true);
  await page.goto("/admin/games");
  await expect(page.locator(".admin-game-identity").first()).toBeVisible();
  await expect(page.locator(".admin-game-identity .tag-chips")).toHaveCount(0);
  const detailURL = await page.locator(".admin-game-identity > a").first().getAttribute("href");
  await page.goto(detailURL!);
  const picker = page.getByRole("combobox", { name: "标签", exact: true });
  await picker.click();
  await expect(page.getByRole("listbox")).toBeVisible();
  const options = page.getByRole("listbox").getByRole("option");
  expect(await options.count()).toBeGreaterThanOrEqual(2);
  for (let i = 0; i < 2; i++) {
    const name = await options.first().innerText();
    await options.first().click();
    await expect(page.getByRole("button", { name: `移除标签“${name}”` })).toBeVisible();
    await expect(page.getByRole("listbox")).toBeVisible();
    await expect(picker).toBeFocused();
  }
  await page.locator(".admin-game-tags .panel-head h2").click();
  await expect(page.getByRole("listbox")).toHaveCount(0);
  // The selected game tags stay local; no assignment is submitted.
  await page.goto("/admin/tags");
  const rowActions = page.locator(".tag-row-actions").first();
  for (const name of ["编辑", "删除"]) {
    const button = rowActions.getByRole("button", { name, exact: true });
    await expect(button).toHaveCSS("border-top-width", "1px");
    await expect(button).toHaveCSS("border-top-style", "solid");
    expect((await button.boundingBox())!.height).toBeGreaterThanOrEqual(32);
  }
  let created = 0;
  await page.route("**/api/v1/admin/tags", async (route) => {
    if (route.request().method() !== "POST") {await route.continue(); return;}
    const { name } = route.request().postDataJSON();
    if (name === "同名标签") {
      await route.fulfill({ status: 409, json: { error: { code: "TAG_NAME_CONFLICT", message: "已存在同名活动标签" } } });
      return;
    }
    created++;
    await route.fulfill({ status: 201, json: { tagId: `preview-${created}`, name, status: "ACTIVE", version: 1, usage: { publishedGameCount: 0, deletedGameCount: 0, reviewDraftCount: 0, pegasusCollectionCount: 0 }, createdAtMs: 1000, updatedAtMs: 1000, deletedAtMs: null } });
  });
  await page.getByRole("button", { name: "新建标签", exact: true }).click();
  const drawer = page.getByRole("dialog", { name: "新建标签" });
  const input = drawer.getByRole("textbox", { name: "标签名称" });
  await drawer.locator(".tag-editor-help").click();
  await page.mouse.move(10, 100);
  await expect(input).toHaveCSS("border-top-width", "1px");
  await expect(input).toHaveCSS("border-top-style", "solid");
  await expect(input).toHaveCSS("background-color", "rgb(255, 255, 255)");
  const originalPreview = await drawer.locator(".tag-normalized-preview").boundingBox();
  for (const name of ["连续新增预览一", "连续新增预览二"]) {
    await input.fill(name);
    await drawer.getByRole("button", { name: "保存标签" }).click();
    await expect(drawer.getByRole("status")).toHaveText(`已创建“${name}”，可继续添加。`);
    await expect(input).toHaveValue(""); await expect(input).toBeFocused();
    expect(await drawer.locator(".tag-normalized-preview").boundingBox()).toEqual(originalPreview);
    const success = (await drawer.getByRole("status").boundingBox())!;
    const save = (await drawer.getByRole("button", { name: "保存标签" }).boundingBox())!;
    expect(success.y).toBeGreaterThanOrEqual(save.y + save.height);
  }
  await input.fill("同名标签");
  await drawer.getByRole("button", { name: "保存标签" }).click();
  const error = drawer.getByRole("alert");
  await expect(error).toContainText("已存在同名活动标签");
  await expect(input).toHaveValue("同名标签");
  expect(await drawer.locator(".tag-normalized-preview").boundingBox()).toEqual(originalPreview);
  expect((await error.boundingBox())!.y).toBeGreaterThanOrEqual((await drawer.getByRole("button", { name: "保存标签" }).boundingBox())!.y + 42);
  await expect(drawer.locator(".responsive-sheet-body .feedback-banner")).toHaveCount(0);
  await drawer.screenshot({ path: testInfo.outputPath("tag-create.png"), scale: "css" });
  await page.mouse.click(10, 100);
  await expect(drawer).toHaveCount(0);
  expect(created).toBe(2);
});
