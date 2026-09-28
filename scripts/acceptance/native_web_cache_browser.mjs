import assert from "node:assert/strict";
import {expect} from "../../web/node_modules/@playwright/test/index.mjs";
import {resumePreview} from "./rpgmaker_preview_actions.mjs";

export function nativeFrame(page) {
  return page.frames().find(frame => /\/__retrom\/(?:tyranoscript\/)?entry$/u.test(new URL(frame.url()).pathname));
}
export async function nativeSnapshot(page) {
  const frame = nativeFrame(page);
  if (!frame) return null;
  return frame.evaluate(() => {
    if (globalThis.Utils?.RPGMAKER_NAME && globalThis.SceneManager?._scene) {
      return {engine: Utils.RPGMAKER_NAME, scene: SceneManager._scene.constructor.name,
        map: globalThis.$gameMap?.mapId(), x: globalThis.$gamePlayer?.x, y: globalThis.$gamePlayer?.y,
        message: globalThis.$gameMessage?.allText(), command: SceneManager._scene._commandWindow?.index()};
    }
    if (globalThis.TYRANO?.kag) return {engine: "TYRANOSCRIPT", scenario: TYRANO.kag.stat.current_scenario,
      order: TYRANO.kag.ftag.current_order_index, message: TYRANO.kag.stat.current_message_str};
    return null;
  });
}
export async function nativeActions(page, actions) {
  assert.ok(Array.isArray(actions) && actions.length <= 100, "NATIVE_CACHE_ACTIONS_INVALID");
  await resumePreview(page);
  const frame = nativeFrame(page);
  assert.ok(frame, "NATIVE_CACHE_FRAME_MISSING");
  const surface = frame.locator("canvas, #tyrano_base").first();
  await surface.evaluate(element => {element.tabIndex = 0; element.focus();});
  for (const action of actions) {
    assert.ok(Number.isInteger(action.waitMs) && action.waitMs >= 0 && action.waitMs <= 10000, "NATIVE_CACHE_WAIT_INVALID");
    if (action.state) {
      assert.ok(typeof action.state === "object" && !Array.isArray(action.state) && Object.keys(action.state).length > 0,
        "NATIVE_CACHE_STATE_INVALID");
      await expect.poll(() => nativeSnapshot(page), {timeout: 180000, intervals: [500]}).toMatchObject(action.state);
    }
    if (action.key) await surface.press(action.key, {delay: 100});
    if (action.selector) await frame.locator(action.selector).click();
    await page.waitForTimeout(action.waitMs);
  }
}
export async function chooseNativeLoading(context, base, gameId, mode) {
  const page = await context.newPage();
  try {
    await page.goto(`${base}/games/${gameId}`, {waitUntil: "domcontentloaded"});
    const choice = page.getByRole("combobox", {name: "内容加载", exact: true});
    await expect.poll(async () => {
      await choice.selectOption(mode);
      return page.evaluate(value => Object.keys(localStorage).some(key =>
        key.startsWith("retrom:v2:user:") && key.endsWith(":player:content-loading") && localStorage.getItem(key) === value), mode);
    }, {timeout: 15000}).toBe(true);
    await page.reload(); await expect(choice).toHaveValue(mode);
  } finally {await page.close();}
}
export async function nativeCacheReceipt(page, digest) {
  return page.evaluate(projectDigest => new Promise((resolve, reject) => {
    const request = indexedDB.open("retrom-content-io-v1");
    request.onerror = () => reject(request.error);
    request.onsuccess = () => {
      const db = request.result, query = db.transaction("objects").objectStore("objects").getAll();
      query.onerror = () => {db.close(); reject(query.error);};
      query.onsuccess = () => {
        db.close(); resolve(query.result.filter(row => row.identity.projectDigest === projectDigest)
          .map(row => ({path: row.identity.logicalPath, state: row.state, sizeBytes: row.sizeBytes, committedBytes: row.committedBytes})));
      };
    };
  }), digest);
}
export async function nativeWorkerRestart(context, page, path) {
  const frame = nativeFrame(page), versions = [], cdp = await context.newCDPSession(page);
  try {
    cdp.on("ServiceWorker.workerVersionUpdated", event => versions.push(...event.versions));
    await cdp.send("ServiceWorker.enable");
    await expect.poll(() => versions.some(version => version.scriptURL.startsWith(new URL(frame.url()).origin) &&
      version.runningStatus === "running")).toBe(true);
    const version = versions.find(value => value.scriptURL.startsWith(new URL(frame.url()).origin) && value.runningStatus === "running");
    await cdp.send("ServiceWorker.stopWorker", {versionId: version.versionId});
  } finally {await cdp.detach();}
  return frame.evaluate(async logicalPath => {
    const url = logicalPath.split("/").map(encodeURIComponent).join("/");
    const response = await fetch(url, {headers: {Range: "bytes=2-18"}, signal: AbortSignal.timeout(15000)});
    return {status: response.status, bytes: (await response.arrayBuffer()).byteLength, contentRange: response.headers.get("Content-Range")};
  }, path);
}
