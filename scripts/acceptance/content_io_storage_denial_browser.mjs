import test from "node:test";
import assert from "node:assert/strict";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {denyContentWorkerStorage} from "./content_io_storage_denial.mjs";

test("[HP-03] BROWSER/storage-denial targets the content Worker and resumes unrelated Workers with their real APIs", {timeout: 30000}, async () => {
  const browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  try {
    const context = await browser.newContext();
    await context.route("https://owned.invalid/", route => route.fulfill({contentType: "text/html", body: "<p>owned storage fault fixture</p>"}));
    const page = await context.newPage(), fault = await denyContentWorkerStorage(context, page);
    await page.goto("https://owned.invalid/");
    const results = await page.evaluate(async () => {
      const code = `const attempt = async call => {try {await call(); return "SUCCESS";} catch(error) {return error.name;}};
        postMessage({opfs: await attempt(() => navigator.storage.getDirectory()), cache: await attempt(() => caches.open("owned-probe"))});`;
      const url = URL.createObjectURL(new Blob([code], {type: "text/javascript"}));
      try {
        const result = {};
        for (const name of ["retrom-content-io-v1", "owned-unrelated-worker"]) {
          const worker = new Worker(url, {type: "module", name});
          try {result[name] = await new Promise((resolve, reject) => {
            worker.onmessage = event => resolve(event.data); worker.onerror = () => reject(new Error("OWNED_WORKER_FAILED"));
          });} finally {worker.terminate();}
        }
        return result;
      } finally {URL.revokeObjectURL(url);}
    });
    assert.deepEqual(results, {"retrom-content-io-v1": {opfs: "NotAllowedError", cache: "NotAllowedError"},
      "owned-unrelated-worker": {opfs: "SUCCESS", cache: "SUCCESS"}});
    assert.deepEqual(await fault.finish(), {injected: true, opfs: "NotAllowedError", cache: "NotAllowedError"});
  } finally {await browser.close();}
});
