import assert from "node:assert/strict";
import {writeFile} from "node:fs/promises";
import {contentSourceMatcher, observeContentIO} from "./content_io_observation.mjs";
import {finalContentMetrics, startContentMeasurement} from "./content_io_measurement.mjs";
import {measureBrowserMemory} from "./content_io_browser_memory.mjs";
import {gamepad, launchCart} from "./fantasy_product_client.mjs";
import {exitContentIOPlayer} from "./content_io_player_exit.mjs";

export const wasm4Observation = "wasm4-owned-square-at-20-20-direction-right-confirm-y40-v1";

export async function observeWasm4Delivery(context, source) {
  await context.addInitScript(source => {
    if (!crypto.subtle) return;
    const digest = crypto.subtle.digest.bind(crypto.subtle);
    crypto.subtle.digest = async (algorithm, input) => {
      const result = await digest(algorithm, input);
      if (input.byteLength === source.sizeBytes) {
        const actual = Array.from(new Uint8Array(result), value => value.toString(16).padStart(2, "0")).join("");
        if (actual === source.sha256) window.__wasm4MeasuredDelivery = {bytes: input.byteLength, digest: actual};
      }
      return result;
    };
  }, source);
}

export async function readWasm4Delivery(page) {
  const deliveries = (await Promise.all(page.frames().map(frame =>
    frame.evaluate(() => window.__wasm4MeasuredDelivery ?? null)))).filter(Boolean);
  assert.equal(deliveries.length, 1, "WASM4_MATERIALIZATION_OBSERVATION_MISSING");
  return deliveries[0];
}

async function squarePosition(canvas) {
  return canvas.evaluate(source => {
    const copy = document.createElement("canvas"); copy.width = 160; copy.height = 160;
    const context = copy.getContext("2d"); context.drawImage(source, 0, 0, 160, 160);
    const pixels = context.getImageData(0, 0, 160, 160).data;
    let x = 160, y = 160, count = 0;
    for (let row = 0; row < 160; row++) for (let column = 0; column < 160; column++) {
      const i = (row * 160 + column) * 4;
      if ([0, 1, 2].some(channel => Math.abs(pixels[i + channel] - pixels[channel]) > 20)) {
        x = Math.min(x, column); y = Math.min(y, row); count++;
      }
    }
    return count >= 80 && count <= 121 ? {x, y} : null;
  });
}

async function initialSquare(page) {
  const deadline = performance.now() + 60000;
  while (performance.now() < deadline) {
    for (const frame of page.frames()) {
      const canvas = frame.locator('canvas[aria-label="WASM-4 game"]');
      if (await canvas.count() && await canvas.isVisible()) {
        const position = await squarePosition(canvas);
        if (position?.x === 20 && position.y === 20) return {canvas, frame};
      }
    }
    assert.equal(await page.getByText("RUNTIME_FAILED", {exact: true}).isVisible(), false);
    await page.waitForTimeout(50);
  }
  throw Error("CONTENT_IO_WASM4_INITIAL_FRAME_TIMEOUT");
}

function baselineMetrics(requests, delivered) {
  for (const row of requests) {
    assert.equal(row.failure, null); assert.equal(row.status, 200);
    assert.equal(row.method, "GET"); assert.equal(row.range, null);
    assert.equal(row.sizeBytes, delivered.bytes);
  }
  // This fixed whole-cart path consumes every successful body before delivering the
  // verified cart to the core. It has no speculative fetches, retries or partial reads.
  return {rangeRequests: 0, wholeRequests: requests.length, headRequests: 0,
    networkBytes: requests.reduce((sum, row) => sum + row.sizeBytes, 0),
    materializedBytes: delivered.bytes, publicPeak: null, closed: null};
}

export async function measureWasm4Launch({browser, context, collector, client, base, gameId, variant, source, directory, preparePage}) {
  const launch = await launchCart(client, gameId); launch.returnTo = `/games/${gameId}`;
  const config = await client.json("GET", `/runtime/launches/${launch.launchId}/config`);
  assert.equal(config.runtime.targetId, "wasm4");
  const resources = config.resources.filter(row => row.role === "game"); assert.equal(resources.length, 1);
  assert.equal(resources[0].sha256, source.sha256); assert.equal(resources[0].sizeBytes, source.sizeBytes);
  const network = observeContentIO(context, contentSourceMatcher(resources, base)); await network.ready;
  const errors = [], page = await context.newPage(); page.on("pageerror", error => errors.push(error.message));
  await preparePage?.(page);
  const clock = startContentMeasurement({observationId: wasm4Observation});
  try {
    await page.goto(base + launch.playUrl, {waitUntil: "domcontentloaded", timeout: 90000});
    const opened = await initialSquare(page); clock.firstFrame();
    await opened.canvas.click(); await gamepad(page, 15, 160);
    const moved = await squarePosition(opened.canvas); assert.ok(moved?.x > 20 && moved.y === 20);
    await gamepad(page, 0, 100);
    const confirmed = await squarePosition(opened.canvas); assert.equal(confirmed?.y, 40); clock.inputReady();
    // Baseline fetchCart hashes the complete materialized cart before core startup.
    // Candidate owns this accounting in its Worker and reports the exact total at CLOSE.
    const delivered = variant === "baseline" ? await readWasm4Delivery(page) : null;
    if (variant === "baseline") {
      assert.equal(delivered?.bytes, source.sizeBytes); assert.equal(delivered?.digest, source.sha256);
    }
    await opened.canvas.screenshot({path: `${directory}/${launch.launchId}.png`});
    const memory = await measureBrowserMemory(browser);
    assert.ok(memory.memories.length > 0 && memory.memories.every(row => !row.shared), "WASM4_UNEXPECTED_MEMORY_TOPOLOGY");
    const wasmHeapBytes = memory.memories.reduce((sum, row) => sum + row.byteLength, 0);
    const storeEvents = (await Promise.all(page.frames().map(frame =>
      frame.evaluate(() => globalThis.__retromContentStoreEvents ?? [])))).flat();
    clock.beginExit(); await exitContentIOPlayer(page, base, launch); clock.exited(); await network.flush();
    const sessions = collector.snapshot(page);
    const totals = variant === "candidate" ? finalContentMetrics(sessions) : baselineMetrics(network.requests, delivered);
    assert.equal(totals.materializedBytes, source.sizeBytes); assert.deepEqual(errors, []);
    return {launchId: launch.launchId, runtime: config.runtime, memory, requests: network.requests, sessions, storeEvents,
      delivered, observed: {moved, confirmed}, metrics: {...clock.timings(), ...totals, wasmHeapBytes,
        processMemoryBytes: memory.processMemoryBytes, processMemoryUnavailableReason: memory.processMemoryUnavailableReason,
        serverSentBytes: null}};
  } catch (error) {
    const frames = [];
    for (const frame of page.frames()) frames.push({
      canvasCount: await frame.locator("canvas").count().catch(() => null),
      delivery: await frame.evaluate(() => window.__wasm4MeasuredDelivery ?? null).catch(() => null),
    });
    await page.screenshot({path: `${directory}/${launch.launchId}-failed.png`}).catch(() => {});
    await writeFile(`${directory}/${launch.launchId}-failed.json`, JSON.stringify({frames, errors, requests: network.requests}, null, 2));
    throw error;
  } finally {network.close(); await page.close();}
}
