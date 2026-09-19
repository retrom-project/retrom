import assert from "node:assert/strict";
import {writeFile} from "node:fs/promises";
import {launchCart, gamepad} from "./fantasy_product_client.mjs";
import {initialRuffleSurface, rufflePosition, focusRuffleSurface} from "./ruffle_product_surface.mjs";
import {contentSourceMatcher, observeContentIO} from "./content_io_observation.mjs";
import {finalContentMetrics, startContentMeasurement} from "./content_io_measurement.mjs";
import {measureBrowserMemory} from "./content_io_browser_memory.mjs";
import {observeVerifiedDeliveries, readVerifiedDeliveries, verifiedFetchMetrics} from "./content_io_verified_delivery.mjs";
import {exitContentIOPlayer} from "./content_io_player_exit.mjs";

export const ruffleObservation = "ruffle-owned-square-x20-direction-right-confirm-native-flush-draft-v1";
const deliveries = new WeakMap();
export async function measureRuffleLaunch({browser, context, collector, client, base, gameId, variant, source, directory, preparePage}) {
  const launch = await launchCart(client, gameId); launch.returnTo = `/games/${gameId}`;
  const config = await client.json("GET", `/runtime/launches/${launch.launchId}/config`);
  assert.equal(config.runtime.targetId, "flash-ruffle");
  const resources = config.resources.filter(row => row.role === "game"); assert.equal(resources.length, 1);
  assert.equal(resources[0].sha256, source.sha256); assert.equal(resources[0].sizeBytes, source.sizeBytes);
  const sources = [{...resources[0], id: "game", url: new URL(resources[0].url, base).href}];
  if (variant === "baseline") {
    if (!deliveries.has(context)) {await observeVerifiedDeliveries(context, sources); deliveries.set(context, source.sha256);}
    assert.equal(deliveries.get(context), source.sha256);
  }
  const network = observeContentIO(context, contentSourceMatcher(sources, base)); await network.ready;
  const errors = [], page = await context.newPage(); page.on("pageerror", error => errors.push(error.message));
  await preparePage?.(page); const clock = startContentMeasurement({observationId: ruffleObservation});
  try {
    await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
    const opened = await initialRuffleSurface(page); clock.firstFrame();
    const staged = page.locator(".player-sync-status").filter({hasText: "数据已暂存在此浏览器，退出时可保存"});
    assert.equal(await staged.count(), 0, "RUFFLE_UNREQUESTED_SAVE");
    await focusRuffleSurface(page, opened); await gamepad(page, 15, 160);
    const moved = await rufflePosition(opened.canvas); assert.ok(moved > opened.position + 10);
    await gamepad(page, 0, 100); await staged.waitFor({state: "attached", timeout: 10000}); clock.inputReady();
    const confirmed = {position: await rufflePosition(opened.canvas), nativeDataStaged: true};
    assert.ok(Math.abs(confirmed.position - moved) < 2);
    const delivered = variant === "baseline" ? await readVerifiedDeliveries(page, sources) : null;
    await opened.canvas.screenshot({path: `${directory}/${launch.launchId}.png`});
    const memory = await measureBrowserMemory(browser);
    assert.ok(memory.memories.length > 0 && memory.memories.every(row => !row.shared), "RUFFLE_UNEXPECTED_MEMORY_TOPOLOGY");
    const storeEvents = (await Promise.all(page.frames().map(frame => frame.evaluate(() => globalThis.__retromContentStoreEvents ?? [])))).flat();
    clock.beginExit(); await exitContentIOPlayer(page, base, launch, "GAME_SAVE"); clock.exited(); await network.flush();
    const sessions = collector.snapshot(page), totals = variant === "candidate" ? finalContentMetrics(sessions) : verifiedFetchMetrics(network.requests, delivered, sources, {cacheHits: true});
    assert.equal(totals.materializedBytes, source.sizeBytes); assert.deepEqual(errors, []);
    return {launchId: launch.launchId, runtime: config.runtime, memory, requests: network.requests, sessions, storeEvents,
      delivered, observed: {initialPosition: opened.position, moved, confirmed}, metrics: {...clock.timings(), ...totals,
        wasmHeapBytes: memory.memories.reduce((sum, row) => sum + row.byteLength, 0),
        processMemoryBytes: memory.processMemoryBytes, processMemoryUnavailableReason: memory.processMemoryUnavailableReason, serverSentBytes: null}};
  } catch (error) {
    await page.screenshot({path: `${directory}/${launch.launchId}-failed.png`}).catch(() => {});
    await writeFile(`${directory}/${launch.launchId}-failed.json`, JSON.stringify({errors, requests: network.requests,
      alerts: await page.getByRole("alert").allTextContents().catch(() => [])}, null, 2)); throw error;
  } finally {network.close(); await page.close();}
}
