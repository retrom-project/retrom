import test from "node:test";
import assert from "node:assert/strict";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {pauseContentCommit} from "./content_io_commit_pause.mjs";
import {measureBrowserMemory} from "./content_io_browser_memory.mjs";

test("[HP-03] BROWSER/commit-pause resumes startup, observes the exact statement, and disables debugging before heap observation", {timeout: 30000}, async testContext => {
  const browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  try {
    const context = await browser.newContext();
    await context.route("https://owned.invalid/", route => route.fulfill({contentType: "text/html", body: "<p>owned commit observation fixture</p>"}));
    const source = `globalThis.ownedMemory = new WebAssembly.Memory({initial: 1});
class Owned {
  async commitBacking() {}
  async run() {
    const written = 3, size = 3, request2 = {kind: "BYTES"};
    const object = {source: {purpose: "GAME"}}, receipt = {}, signal = {};
    await this.commitBacking(object, receipt, signal);
    postMessage("committed");
  }
}
self.onmessage = () => new Owned().run().catch(error => postMessage({error: String(error)}));
postMessage("ready");`;
    const page = await context.newPage(); let callbacks = 0;
    const pause = await pauseContentCommit(context, page, source, async value => {
      assert.deepEqual(value, {written: 3, size: 3, kind: "BYTES"}); callbacks++;
      return {observed: true};
    });
    await page.goto("https://owned.invalid/");
    let timer;
    const completed = page.evaluate(async source => {
      const url = URL.createObjectURL(new Blob([source], {type: "text/javascript"}));
      globalThis.ownedWorker = new Worker(url, {type: "module", name: "retrom-content-io-v1"});
      try {return await new Promise((resolve, reject) => {
        ownedWorker.onmessage = event => resolve(event.data); ownedWorker.onerror = () => reject(new Error("OWNED_WORKER_FAILED"));
      });} finally {URL.revokeObjectURL(url);}
    }, source);
    try {
      const deadline = new Promise((_, reject) => {timer = setTimeout(() => reject(new Error("OWNED_COMMIT_WORKER_TIMEOUT")), 10000);});
      assert.equal(await Promise.race([completed, deadline]), "ready");
      const until = performance.now() + 5000;
      while (!pause.snapshot().events.includes("breakpoint-installed") && performance.now() < until) await page.waitForTimeout(10);
      assert.ok(pause.snapshot().events.includes("breakpoint-installed"));
      const committed = page.evaluate(() => new Promise(resolve => {
        ownedWorker.onmessage = event => resolve(event.data); ownedWorker.postMessage("run");
      }));
      assert.equal(await Promise.race([committed, deadline]), "committed");
    } finally {clearTimeout(timer); testContext.diagnostic(JSON.stringify(pause.snapshot()));}
    const observed = await pause.finish(); assert.equal(callbacks, 1); assert.equal(observed.observed, true);
    assert.equal(pause.snapshot().events.at(-1), "resumed-and-disabled");
    const memory = await measureBrowserMemory(browser);
    assert.deepEqual(memory.memories.map(row => row.byteLength), [65536]);
  } finally {await browser.close();}
});
