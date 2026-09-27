import assert from "node:assert/strict";
import {launchCart} from "./fantasy_product_client.mjs";
import {openComputer, closeComputer, pictureComputer} from "./computer_product_browser.mjs";
import {bootBatBall, moveBatBall} from "./bbc_product_browser.mjs";
import {bootSafari, moveSafari} from "./samcoupe_product_browser.mjs";
import {measureBrowserMemory} from "./content_io_browser_memory.mjs";

export const computerObservation = target => target === "bbc-jsbeeb" ? "bbc-welcome-batball-start-paddle-right-v1" : "sam-safari-start-cursor-player-right-v1";
export async function measureComputer({browser, context, collector, client, base, target, gameId, sources, directory, preparePage}) {
  const launch = await launchCart(client, gameId); launch.returnTo = `/games/${gameId}`;
  const opened = await openComputer(context, base, launch, target, sources, preparePage);
  const initial = target === "bbc-jsbeeb" ? await bootBatBall(opened) : await bootSafari(opened);
  const firstFrameMs = performance.now() - opened.startedAt;
  const input = target === "bbc-jsbeeb" ? await moveBatBall(opened) : await moveSafari(opened);
  const inputReadyMs = performance.now() - opened.startedAt;
  const screenshot = await pictureComputer(opened, directory, launch.launchId);
  const memory = await measureBrowserMemory(browser);
  const exitAt = performance.now();
  const result = await closeComputer(opened, base, collector, target === "samcoupe" ? "GAME_SAVE" : "INSTANT");
  const exitMs = performance.now() - exitAt;
  const metrics = {...result.metrics, firstFrameMs, inputReadyMs, exitMs, serverSentBytes: null,
    processMemoryBytes: memory.processMemoryBytes, processMemoryUnavailableReason: memory.processMemoryUnavailableReason,
    wasmHeapBytes: memory.memories.reduce((sum, row) => sum + row.byteLength, 0)};
  assert.ok(metrics.inputReadyMs >= metrics.firstFrameMs);
  return {...result, initial, input, screenshot, memory, metrics};
}
