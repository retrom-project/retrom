import test from "node:test";
import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {observeWasm4Delivery, readWasm4Delivery} from "./wasm4_performance_browser.mjs";

test("[HP-03] BROWSER/wasm4-materialization observes the real parent digest while the game belongs to its iframe", {timeout: 30000}, async () => {
  const browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  try {
    const context = await browser.newContext(), bytes = [1, 4, 9];
    const sha256 = createHash("sha256").update(Buffer.from(bytes)).digest("hex");
    await observeWasm4Delivery(context, {sizeBytes: bytes.length, sha256});
    await context.route("https://owned.invalid/", route => route.fulfill({contentType: "text/html", body: "<iframe srcdoc='<canvas></canvas>'></iframe>"}));
    const page = await context.newPage(); await page.goto("https://owned.invalid/");
    await assert.rejects(readWasm4Delivery(page), /OBSERVATION_MISSING/u);
    const hash = await page.evaluate(async bytes => {
      await crypto.subtle.digest("SHA-256", Uint8Array.of(9, 4, 1));
      if (window.__wasm4MeasuredDelivery) throw Error("UNRELATED_HASH_CAPTURED");
      const result = await crypto.subtle.digest("SHA-256", Uint8Array.from(bytes));
      return Array.from(new Uint8Array(result), value => value.toString(16).padStart(2, "0")).join("");
    }, bytes);
    assert.equal(hash, sha256);
    const game = page.frames().find(frame => frame !== page.mainFrame()); assert.ok(game);
    assert.equal(await game.evaluate(() => window.__wasm4MeasuredDelivery ?? null), null);
    assert.deepEqual(await readWasm4Delivery(page), {bytes: 3, digest: sha256});
    // Multiple observations are ambiguous; never select an arbitrary realm's count.
    await game.evaluate(bytes => crypto.subtle.digest("SHA-256", Uint8Array.from(bytes)), bytes);
    await assert.rejects(readWasm4Delivery(page), /OBSERVATION_MISSING/u);
  } finally {await browser.close();}
});
