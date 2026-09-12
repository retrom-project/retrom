import assert from "node:assert/strict";

export function isPSPDiscRequest(url) {
  return new URL(url).pathname.startsWith("/runtime/content/game/");
}

export function observePSPRange(context) {
  const requests = [], pending = new Set();
  context.on("response", response => {
    if (!isPSPDiscRequest(response.url())) return;
    const task = (async () => {
      const failure = await response.finished(), headers = await response.allHeaders();
      requests.push({path: new URL(response.url()).pathname, status: response.status(), failure,
        range: (await response.request().allHeaders()).range ?? null, contentRange: headers["content-range"],
        sizeBytes: Number(headers["content-length"] ?? 0), etag: headers.etag});
    })();
    pending.add(task); void task.finally(() => pending.delete(task));
  });
  return {requests, async flush() {await Promise.all([...pending]);}};
}

export function rangeSummary(requests, source) {
  const path = new URL(source.url, "http://localhost").pathname;
  const matches = requests.filter(item => item.path === path);
  for (const item of matches) {
    assert.equal(item.failure, null, "PSP_RANGE_TRANSFER_FAILED");
    assert.equal(item.status, 206, "PSP_WHOLE_DISC_RESPONSE");
    const range = /^bytes=(\d+)-(\d+)$/u.exec(item.range ?? "");
    assert.ok(range, "PSP_RANGE_MISSING");
    const start = Number(range[1]), end = Number(range[2]);
    assert.ok(start >= 0 && end >= start && end < source.sizeBytes && end - start + 1 <= 262144, "PSP_RANGE_UNBOUNDED");
    assert.equal(item.sizeBytes, end - start + 1, "PSP_RANGE_LENGTH_MISMATCH");
    assert.equal(item.contentRange, `bytes ${start}-${end}/${source.sizeBytes}`, "PSP_RANGE_IDENTITY_MISMATCH");
    assert.equal(item.etag, `"sha256-${source.sha256}"`, "PSP_RANGE_ETAG_MISMATCH");
  }
  return {requests: matches.length, downloadedBytes: matches.reduce((sum, item) => sum + item.sizeBytes, 0),
    discBytes: source.sizeBytes};
}

export function assertPartialStartup(summary) {
  assert.ok(summary.requests > 0 && summary.downloadedBytes > 0, "PSP_COLD_READ_MISSING");
  assert.ok(summary.downloadedBytes < summary.discBytes, "PSP_STARTUP_DOWNLOADED_WHOLE_DISC");
  return {...summary, fraction: summary.downloadedBytes / summary.discBytes};
}
