import assert from "node:assert/strict";

export function assertNativePreload(files, requests, receipt) {
  const indexed = new Map(files.map(file => [new URL(file.url, "http://localhost").pathname, file]));
  const downloaded = new Map();
  for (const request of requests) {
    const file = indexed.get(request.path);
    assert.ok(file, "NATIVE_CACHE_UNINDEXED_DOWNLOAD");
    if ([408, 429, 502, 503, 504].includes(request.status)) continue; // Retry bodies are not game bytes.
    assert.equal(request.failure, null, "NATIVE_CACHE_TRANSFER_FAILED");
    // The shared preloader permits whole reads only for files at most 1 MiB.
    const boundedWhole = request.status === 200 && file.sizeBytes <= 1048576 && request.sizeBytes === file.sizeBytes;
    assert.ok(request.status === 206 || boundedWhole, "NATIVE_CACHE_RANGE_REQUIRED");
    assert.ok(request.sizeBytes <= 2097152, "NATIVE_CACHE_DOWNLOAD_WINDOW_EXCEEDED");
    downloaded.set(request.path, (downloaded.get(request.path) ?? 0) + request.sizeBytes);
  }
  assert.equal(receipt.length, files.length, "NATIVE_CACHE_RECEIPT_INCOMPLETE");
  const cached = new Map(receipt.map(row => [row.path, row]));
  for (const file of files) {
    const row = cached.get(file.path);
    assert.ok(row && row.state === "COMPLETE" && row.committedBytes === file.sizeBytes && row.sizeBytes === file.sizeBytes,
      "NATIVE_CACHE_FILE_INCOMPLETE");
    assert.equal(downloaded.get(new URL(file.url, "http://localhost").pathname) ?? 0, file.sizeBytes, "NATIVE_CACHE_DOWNLOAD_COVERAGE");
  }
}
export function assertNativeLocalResponses(responses) {
  assert.ok(responses.length > 0 && responses.every(row => row.local), "NATIVE_CACHE_BROWSER_NETWORK_FALLBACK");
}
export function assertNativeInput(before, after) {
  assert.ok(before?.engine && after?.engine, "NATIVE_CACHE_ENGINE_UNAVAILABLE");
  assert.notDeepEqual(after, before, "NATIVE_CACHE_INPUT_DID_NOT_ADVANCE");
}
