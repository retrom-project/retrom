import "./ruffle_surface_browser.mjs";
import test from "node:test";
import assert from "node:assert/strict";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {observeContentStoreEvents} from "./content_store_events.mjs";
import "./content_io_memory_browser.mjs";
import "./wasm4_measurement_browser.mjs";
import "./content_io_storage_denial_browser.mjs";
import "./content_io_commit_pause_browser.mjs";
import "./content_io_verified_delivery_browser.mjs";

test("[HP-03] BROWSER/host-metrics retains the complete final message after the runtime iframe disappears", {timeout: 30000}, async () => {
  assert.ok(process.env.RETROM_CHROME_EXECUTABLE, "CONTENT_IO_CHROME_REQUIRED");
  const browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  try {
    const context = await browser.newContext();
    const collector = await observeContentStoreEvents(context, {retain: true});
    const page = await context.newPage();
    await page.goto("data:text/html,<iframe srcdoc='<p>owned observer fixture</p>'></iframe>");
    const frame = page.frames().find(value => value !== page.mainFrame()); assert.ok(frame);
    const sessionId = "12345678-1234-4123-8123-123456789012";
    await frame.evaluate(sessionId => {
      dispatchEvent(new CustomEvent("retrom:runtime-diagnostic", {detail: {code: "CONTENT_IO_METRICS",
        message: JSON.stringify({sessionId, operation: "CLOSE", codeNumber: 0,
          counts: {networkBytes: 17, channels: 0, leases: 0, peakChannels: 1, secret: "must-not-retain"}})}}));
    }, sessionId);
    const deadline = performance.now() + 5000;
    while (!collector.snapshot(page)[sessionId] && performance.now() < deadline) await page.waitForTimeout(10);
    const snapshot = collector.snapshot(page)[sessionId]; assert.ok(snapshot);
    await page.locator("iframe").evaluate(element => element.remove());
    assert.equal(page.frames().length, 1);
    assert.deepEqual(collector.snapshot(page)[sessionId].closeCounts, {networkBytes: 17, channels: 0, leases: 0, peakChannels: 1});
    assert.equal(snapshot.source, "HOST"); assert.equal(snapshot.lastOperation, "CLOSE"); assert.equal(snapshot.lastCodeNumber, 0);
    assert.ok(!JSON.stringify(snapshot).includes("must-not-retain"));
    await page.close(); assert.deepEqual(collector.snapshot(page)[sessionId], snapshot);
  } finally {await browser.close();}
});
