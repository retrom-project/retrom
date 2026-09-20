import test from "node:test";
import assert from "node:assert/strict";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {measureBrowserMemory} from "./content_io_browser_memory.mjs";

test("[HP-03] BROWSER/wasm-memory measures live page and Worker heaps and actual Chrome process RSS", {timeout: 45000}, async context => {
  assert.ok(process.env.RETROM_CHROME_EXECUTABLE, "CONTENT_IO_CHROME_REQUIRED");
  const browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  try {
    const page = await browser.newPage(); await page.goto("data:text/html,<p>owned memory observation fixture</p>");
    await page.evaluate(async () => {
      globalThis.ownedMemory = new WebAssembly.Memory({initial: 2});
      const url = URL.createObjectURL(new Blob(["globalThis.ownedMemory = new WebAssembly.Memory({initial: 3}); postMessage('ready');"], {type: "text/javascript"}));
      globalThis.ownedWorker = new Worker(url);
      await new Promise(resolve => {globalThis.ownedWorker.onmessage = resolve;}); URL.revokeObjectURL(url);
    });
    const measured = await measureBrowserMemory(browser);
    assert.deepEqual(measured.memories.map(row => [row.targetType, row.byteLength, row.shared]).sort(), [["page", 131072, false], ["worker", 196608, false]]);
    assert.ok(measured.processMemoryBytes > 0); assert.equal(measured.processMemoryUnavailableReason, null);
    assert.ok(measured.processes.length >= 2); context.diagnostic(JSON.stringify(measured));
  } finally {await browser.close();}
});
