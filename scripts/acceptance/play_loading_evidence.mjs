import assert from "node:assert/strict";
import {contentSourceMatcher, observeContentIO, rangeSummary} from "./content_io_observation.mjs";

export async function observePlayLoading(context, base, launch, evidence) {
  const response = await context.request.get(`${base}/runtime/launches/${launch.launchId ?? launch.previewId}/config`);
  assert.equal(response.status(), 200, "PLAY_CONFIG_FAILED");
  const config = await response.json();
  assert.equal(config.runtime.targetId, "play-ps2");
  const sources = config.resources.filter(source => source.role === "game");
  assert.equal(sources.length, 1); const source = sources[0];
  assert.equal(source.kind, "SEEKABLE_BLOB");
  const observer = observeContentIO(context, contentSourceMatcher(sources, base));
  await observer.ready;
  let countedRequests = 0, countedBytes = 0;
  return {config, requests: observer.requests, close: () => observer.close(), async snapshot() {
    await observer.flush();
    const summary = rangeSummary(observer.requests, source);
    evidence.rangeRequests += summary.requests - countedRequests;
    evidence.rangeBytes += summary.downloadedBytes - countedBytes;
    countedRequests = summary.requests; countedBytes = summary.downloadedBytes;
    return summary;
  }};
}
