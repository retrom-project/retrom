import test from "node:test";
import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {chromium} from "../../web/node_modules/playwright/index.mjs";
import {observeVerifiedDeliveries, readVerifiedDeliveries, verifiedFetchMetrics} from "./content_io_verified_delivery.mjs";
import {observeContentIO, contentSourceMatcher} from "./content_io_observation.mjs";

test("[HP-03] BROWSER/verified deliveries include parent and iframe hashes and distinguish a real module import", {timeout: 30000}, async () => {
  const browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  try {
    const context = await browser.newContext(), origin = "https://owned.invalid";
    const bytes = {game: "owned game bytes", wasm: "owned asset data", module: "export const ready = true;"};
    const sources = Object.entries(bytes).map(([id, value]) => ({id, url: `${origin}/${id}`, sizeBytes: Buffer.byteLength(value),
      sha256: createHash("sha256").update(value).digest("hex")}));
    await context.route(`${origin}/**`, route => {
      const key = new URL(route.request().url()).pathname.slice(1);
      return route.fulfill({contentType: key === "module" ? "text/javascript" : key ? "application/octet-stream" : "text/html",
        body: bytes[key] ?? "<iframe srcdoc='<p>owned frame</p>'></iframe>"});
    });
    await observeVerifiedDeliveries(context, sources);
    const network = observeContentIO(context, contentSourceMatcher(sources, origin)); await network.ready;
    const page = await context.newPage(); await page.goto(origin);
    const frame = page.frames().find(value => value !== page.mainFrame()); assert.ok(frame);
    const verify = async url => crypto.subtle.digest("SHA-256", await (await fetch(url)).arrayBuffer());
    await frame.evaluate(verify, `${origin}/game`);
    await page.evaluate(verify, `${origin}/wasm`); await page.evaluate(verify, `${origin}/module`);
    assert.equal(await page.evaluate(async url => (await import(url)).ready, `${origin}/module`), true);
    const observed = await readVerifiedDeliveries(page, sources); await network.flush();
    const metrics = verifiedFetchMetrics(network.requests, observed, sources);
    assert.equal(metrics.wholeRequests, 3); assert.equal(metrics.materializedBytes, sources.reduce((sum, row) => sum + row.sizeBytes, 0));
    assert.equal(network.requests.filter(row => row.resourceType === "script").length, 1);
    await page.evaluate(verify, `${origin}/game`);
    await assert.rejects(readVerifiedDeliveries(page, sources), /DELIVERY_MISSING_OR_REPEATED/u);
    await network.flush(); network.close();
  } finally {await browser.close();}
});

test("[HP-03] BROWSER/verified legacy CacheStorage reads retain materialized bytes with zero warm network requests", {timeout: 30000}, async () => {
  const browser = await chromium.launch({executablePath: process.env.RETROM_CHROME_EXECUTABLE, headless: true});
  try {
    const context = await browser.newContext(), origin = "https://owned.invalid", body = "owned cache bytes";
    const sources = [{id: "game", url: origin + "/game", sizeBytes: Buffer.byteLength(body),
      sha256: createHash("sha256").update(body).digest("hex")}];
    await context.route(origin + "/**", route => route.fulfill({contentType: "text/html",
      body: new URL(route.request().url()).pathname === "/game" ? body : "<p>owned cache fixture</p>"}));
    await observeVerifiedDeliveries(context, sources);
    const cold = await context.newPage(); await cold.goto(origin);
    await cold.evaluate(async url => (await caches.open("owned-legacy-v1")).put(url, await fetch(url)), sources[0].url);
    await cold.close();
    const network = observeContentIO(context, contentSourceMatcher(sources, origin)); await network.ready;
    const warm = await context.newPage(); await warm.goto(origin);
    await warm.evaluate(async url => {
      const response = await (await caches.open("owned-legacy-v1")).match(url);
      if (!response) throw Error("CACHE_FIXTURE_MISSING");
      await crypto.subtle.digest("SHA-256", await response.arrayBuffer());
    }, sources[0].url);
    const observed = await readVerifiedDeliveries(warm, sources); await network.flush();
    const metrics = verifiedFetchMetrics(network.requests, observed, sources, {cacheHits: true});
    assert.equal(metrics.networkBytes, 0); assert.equal(metrics.wholeRequests, 0); assert.equal(metrics.materializedBytes, Buffer.byteLength(body));
    assert.throws(() => verifiedFetchMetrics(network.requests, observed, sources), /BASELINE_FETCH_COUNT/);
    network.close();
  } finally {await browser.close();}
});
