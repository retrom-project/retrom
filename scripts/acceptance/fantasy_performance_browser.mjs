import assert from "node:assert/strict";
import {writeFile} from "node:fs/promises";
import {observeContentIO, contentSourceMatcher} from "./content_io_observation.mjs";
import {observeVerifiedDeliveries, readVerifiedDeliveries, verifiedFetchMetrics} from "./content_io_verified_delivery.mjs";
import {finalContentMetrics, startContentMeasurement} from "./content_io_measurement.mjs";
import {measureBrowserMemory} from "./content_io_browser_memory.mjs";
import {gamepad, launchCart} from "./fantasy_product_client.mjs";
import {spriteState} from "./fantasy_fixture.mjs";
import {exitContentIOPlayer} from "./content_io_player_exit.mjs";

const contexts = new WeakMap();
const colors = {tic80: {initial: [41, 54, 111], confirmed: [115, 239, 247]}, fake08: {initial: [255, 0, 74], confirmed: [0, 230, 49]}};
export const fantasyObservation = core => `${core}-owned-x20-palette8-direction-right-confirm-palette11-v1`;
function contentSources(config, base, core, source, assets) {
  const game = config.resources.filter(row => row.role === "game"); assert.equal(game.length, 1);
  assert.equal(game[0].sha256, source.sha256); assert.equal(game[0].sizeBytes, source.sizeBytes);
  const assetBase = new URL(config.runtime.runtimeBaseUrl, base); assert.equal(assetBase.origin, new URL(base).origin);
  assert.equal(assets.length, 2);
  const sources = [{id: "game", ...source, url: new URL(game[0].url, base).href}];
  for (const entry of assets) {
    assert.ok(entry.path.startsWith(`assets/${core}/`));
    sources.push({id: entry.id, sha256: entry.sha256, sizeBytes: entry.sizeBytes, url: new URL(entry.path, assetBase).href});
  }
  return sources;
}
async function observeDeliveryOnce(context, sources) {
  const signature = JSON.stringify(sources.map(({id, sha256, sizeBytes}) => ({id, sha256, sizeBytes})));
  if (contexts.has(context)) {assert.equal(contexts.get(context), signature); return;}
  await observeVerifiedDeliveries(context, sources); contexts.set(context, signature);
}
async function initialScene(page, core) {
  const deadline = performance.now() + 60000;
  while (performance.now() < deadline) {
    for (const frame of page.frames()) {
      const canvas = frame.locator(`canvas[aria-label="${core} game"]`);
      if (await canvas.count() && await canvas.isVisible()) {
        const state = await spriteState(canvas);
        if (state.x === 20 && state.color.every((value, index) => value === colors[core].initial[index])) return {canvas, state};
      }
    }
    assert.equal(await page.getByText("RUNTIME_FAILED", {exact: true}).isVisible(), false);
    await page.waitForTimeout(50);
  }
  throw new Error("CONTENT_IO_FANTASY_INITIAL_FRAME_TIMEOUT");
}
export async function measureFantasyLaunch({browser, context, collector, client, base, core, gameId, variant, source, assets, directory, preparePage}) {
  const launch = await launchCart(client, gameId); launch.returnTo = `/games/${gameId}`;
  const config = await client.json("GET", `/runtime/launches/${launch.launchId}/config`); assert.equal(config.runtime.targetId, core);
  const sources = contentSources(config, base, core, source, assets); await observeDeliveryOnce(context, sources);
  const network = observeContentIO(context, contentSourceMatcher(sources, base)); await network.ready;
  const errors = [], page = await context.newPage(); page.on("pageerror", error => errors.push(error.message));
  await preparePage?.(page);
  const clock = startContentMeasurement({observationId: fantasyObservation(core)});
  try {
    await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
    const opened = await initialScene(page, core); clock.firstFrame();
    await opened.canvas.click(); await gamepad(page, 15, 160);
    const moved = await spriteState(opened.canvas); assert.ok(moved.x > 20); assert.deepEqual(moved.color, colors[core].initial);
    await gamepad(page, 0, 100); const confirmed = await spriteState(opened.canvas);
    assert.equal(confirmed.x, moved.x); assert.deepEqual(confirmed.color, colors[core].confirmed); clock.inputReady();
    const delivered = variant === "baseline" ? await readVerifiedDeliveries(page, sources) : null;
    await opened.canvas.screenshot({path: `${directory}/${launch.launchId}.png`});
    const memory = await measureBrowserMemory(browser);
    assert.ok(memory.memories.length > 0 && memory.memories.every(row => !row.shared), "FANTASY_UNEXPECTED_MEMORY_TOPOLOGY");
    const storeEvents = (await Promise.all(page.frames().map(frame => frame.evaluate(() => globalThis.__retromContentStoreEvents ?? [])))).flat();
    clock.beginExit(); await exitContentIOPlayer(page, base, launch, core === "tic80" ? "GAME_SAVE" : "INSTANT"); clock.exited();
    await network.flush(); const sessions = collector.snapshot(page);
    const totals = variant === "candidate" ? finalContentMetrics(sessions) : verifiedFetchMetrics(network.requests, delivered, sources);
    assert.equal(totals.materializedBytes, sources.reduce((sum, row) => sum + row.sizeBytes, 0)); assert.deepEqual(errors, []);
    return {launchId: launch.launchId, runtime: config.runtime, memory, requests: network.requests, sessions, storeEvents, delivered,
      sources: sources.map(({url, ...row}) => ({...row, path: new URL(url).pathname})), observed: {initial: opened.state, moved, confirmed},
      metrics: {...clock.timings(), ...totals, wasmHeapBytes: memory.memories.reduce((sum, row) => sum + row.byteLength, 0),
        processMemoryBytes: memory.processMemoryBytes, processMemoryUnavailableReason: memory.processMemoryUnavailableReason, serverSentBytes: null}};
  } catch (error) {
    await page.screenshot({path: `${directory}/${launch.launchId}-failed.png`}).catch(() => {});
    await writeFile(`${directory}/${launch.launchId}-failed.json`, JSON.stringify({errors, requests: network.requests}, null, 2)); throw error;
  } finally {network.close(); await page.close();}
}
