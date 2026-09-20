import {observeContentIO} from "./content_io_observation.mjs";
import assert from "node:assert/strict";

export function isPSPDiscRequest(url) {
  return new URL(url).pathname.startsWith("/runtime/content/game/");
}

export async function observePSPRange(context) {
  const observer = observeContentIO(context, isPSPDiscRequest);
  await observer.ready; return observer;
}
export {rangeSummary} from "./content_io_observation.mjs";

export function assertPartialStartup(summary) {
  assert.ok(summary.requests > 0 && summary.downloadedBytes > 0, "PSP_COLD_READ_MISSING");
  assert.ok(summary.downloadedBytes < summary.discBytes, "PSP_STARTUP_DOWNLOADED_WHOLE_DISC");
  return {...summary, fraction: summary.downloadedBytes / summary.discBytes};
}
