import assert from "node:assert/strict";
import {validateFetchExtent, assertNoRepeatedBlocks} from "./content_fetch_policy.mjs";
import {observeContentIO, rangeSummary} from "./content_io_observation.mjs";

export async function observeNeoCDRanges(context) {
  const sources = new Map();
  const observer = observeContentIO(context, url => sources.has(url));
  await observer.ready;
  async function summarize(url) {
    await observer.flush();
    const source = sources.get(url); assert.ok(source, "NEOCD_DISC_UNDECLARED");
    const summary = rangeSummary(observer.requests, source);
    return {requests: summary.requests, bytes: summary.downloadedBytes, fetchPolicy: summary.fetchPolicy};
  }
  return {
    register(source, base) {
      const url = new URL(source.url, base).href;
      const previous = sources.get(url);
      if (previous) {assert.equal(previous.sha256, source.sha256); assert.equal(previous.sizeBytes, source.sizeBytes);}
      sources.set(url, {...source, url});
    },
    close: () => observer.close(),
    snapshot: summarize,
    async verify(url, sizeBytes) {
      const summary = await summarize(url);
      assert.equal(sources.get(url).sizeBytes, sizeBytes);
      const entries = observer.requests.filter(request => request.path === new URL(url).pathname && request.method !== "HEAD");
      assert.ok(entries.length > 0, "NEOCD_RANGE_REQUESTS_MISSING");
      assertNoRepeatedBlocks(entries.map(entry => validateFetchExtent(entry, sources.get(url), summary.fetchPolicy)), "disc");
      if (sizeBytes > 32 * 1024 * 1024) assert.ok(summary.bytes < sizeBytes / 2, "NEOCD_RANGE_DOWNLOADED_TOO_MUCH");
      return {...summary, ranges: entries.map(({path: _path, ...entry}) => entry)};
    },
  };
}
