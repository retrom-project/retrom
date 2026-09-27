import assert from "node:assert/strict";
import {launchCart} from "./fantasy_product_client.mjs";
import {openDOS} from "./dosbox_product_browser.mjs";
import {bootDoom, moveAndFireDoom} from "./dosbox_doom_actions.mjs";
import {closeComputer, pictureComputer} from "./computer_product_browser.mjs";
import {measureBrowserRSS} from "./content_io_browser_memory.mjs";

export const dosObservation = "dos-doom2-newgame-view-right-primary-fire-v1";
export async function measureDOS({browser, context, collector, client, base, gameId, content, directory, preparePage}) {
  const launch = await launchCart(client, gameId, null, process.env.RETROM_DOS_ENTRY); launch.returnTo = `/games/${gameId}`;
  const opened = await openDOS(context, base, launch, preparePage);
  assert.equal(opened.contentDigest, content.contentDigest); assert.equal(opened.source.sizeBytes, content.sizeBytes);
  const initial = await bootDoom(opened), firstFrameMs = performance.now() - opened.startedAt;
  const input = await moveAndFireDoom(opened), inputReadyMs = performance.now() - opened.startedAt;
  const screenshot = await pictureComputer(opened, directory, launch.launchId);
  const heap = await opened.frame.evaluate(() => {
    const buffer = globalThis.EJS_emulator.Module.HEAPU8.buffer;
    return {byteLength: buffer.byteLength, shared: Object.prototype.toString.call(buffer) === "[object SharedArrayBuffer]"};
  });
  assert.ok(heap.byteLength > 0 && heap.shared, "DOS_SHARED_HEAP_MISSING");
  // The main Module and the native pthread wrap the same shared Wasm backing.
  const memory = {heap, ...await measureBrowserRSS(browser)};
  await opened.flush(); const ranges = opened.rangeSummary();
  const exiting = performance.now(), result = await closeComputer(opened, base, collector), exitMs = performance.now() - exiting;
  assert.equal(result.metrics.wholeRequests, 0, "DOS_WHOLE_CONTENT_FALLBACK");
  return {...result, initial, input, screenshot, ranges, memory, metrics: {...result.metrics, firstFrameMs, inputReadyMs, exitMs,
    serverSentBytes: null, wasmHeapBytes: heap.byteLength, processMemoryBytes: memory.processMemoryBytes,
    processMemoryUnavailableReason: memory.processMemoryUnavailableReason}};
}
