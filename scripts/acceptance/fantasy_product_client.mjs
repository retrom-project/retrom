import assert from "node:assert/strict";
import {createProductClient, singleFile, reviewForImport} from "./rpgmaker_security_upload.mjs";
import {revealPreviewToolbar} from "./rpgmaker_preview_actions.mjs";
const capabilities = {secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true};
export async function fantasyClient(context, baseUrl) {
  const response = await context.request.post(`${baseUrl}/api/v1/auth/login`, {
    headers: {Origin: baseUrl}, data: {username: process.env.RETROM_ACCEPTANCE_USERNAME,
      password: process.env.RETROM_ACCEPTANCE_PASSWORD},
  });
  assert.equal(response.status(), 200, "FANTASY_LOGIN_FAILED");
  return createProductClient(context, baseUrl, (await response.json()).csrfToken);
}
export async function importCart(client, core, filename) {
  const platform = core === "tic80" ? "tic80" : "pico8";
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const platforms = await client.json("GET", `/api/v1/admin/platform-instances?platformId=${platform}&limit=100`);
  const instance = platforms.items.find((item) => item.enabled && item.defaultCoreId === core);
  assert.ok(instance, "FANTASY_PLATFORM_MISSING");
  const uploadId = await client.upload(singleFile(filename), "FILES", "GENERAL");
  const imported = await client.json("POST", "/api/v1/admin/imports", {
    headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: instance.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []},
  });
  return reviewForImport(client, imported.importJobId);
}
export async function previewCart(client, itemId) {
  return client.json("POST", `/api/v1/admin/reviews/${itemId}/previews`, {
    headers: client.writeHeaders(), expected: 201, data: {clientCapabilities: capabilities},
  });
}
export async function approveCart(client, itemId) {
  const snapshot = await client.raw("GET", `/api/v1/admin/reviews/${itemId}`);
  assert.equal(snapshot.status(), 200); assert.ok(snapshot.headers().etag);
  return client.json("POST", `/api/v1/admin/reviews/${itemId}/approve`, {
    headers: {...client.writeHeaders(), "If-Match": snapshot.headers().etag}, data: {}, expected: 201,
  });
}
export async function launchCart(client, gameId, saveStateId = null) {
  return client.json("POST", "/api/v1/launches", {
    headers: client.writeHeaders(), expected: 201,
    data: {gameId, coreId: null, saveStateId, dosEntry: null, returnTo: `/games/${gameId}`, clientCapabilities: capabilities},
  });
}
export async function runtimeCanvas(page, core) {
  const deadline = Date.now() + 60000;
  while (Date.now() < deadline) {
    for (const frame of page.frames()) {
      const canvas = frame.locator(`canvas[aria-label="${core} game"]`);
      if (await canvas.isVisible()) {await canvas.click(); return canvas;}
    }
    const alert = await page.getByRole("alert").allTextContents();
    if (alert.some((text) => /FANTASY_|PROVIDER_|RUNTIME_FAILED/u.test(text)) || await page.getByText("RUNTIME_FAILED", {exact: true}).isVisible()) {throw Error("FANTASY_RUNTIME_FAILED:" + alert.join(" "));}
    await page.waitForTimeout(100);
  }
  throw Error("FANTASY_CANVAS_TIMEOUT");
}
export async function gamepad(page, button, milliseconds = 250) {
  for (const pressed of [true, false]) {
    await Promise.all(page.frames().map((frame) => frame.evaluate(({button, pressed}) => {
      globalThis.__retromTestGamepad?.button(button, pressed);
    }, {button, pressed})));
    await page.waitForTimeout(pressed ? milliseconds : 100);
  }
}
export async function saveCart(page, launchId, core) {
  await revealPreviewToolbar(page);
  const response = page.waitForResponse((entry) => entry.request().method() === "POST" &&
    entry.url().includes(`/runtime/launches/${launchId}/save-states`), {timeout: 30000})
    .then((value) => ({value}), (error) => ({error}));
  if (core === "tic80") {
    await page.getByRole("button", {name: "返回并退出游戏", exact: true}).click();
    await page.getByRole("button", {name: "存档并退出", exact: true}).click();
  } else {await page.getByRole("button", {name: "创建存档", exact: true}).click();}
  const result = await response; if (result.error) {throw result.error;}
  const saved = result.value; assert.equal(saved.status(), 201, "FANTASY_SAVE_FAILED");
  return saved.json();
}
