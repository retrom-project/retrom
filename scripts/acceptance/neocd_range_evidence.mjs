import assert from "node:assert/strict";

export function observeNeoCDRanges(context) {
  const requests = [], pending = [];
  context.on("response", response => {
    const request = response.request();
    if (!request.headers().range) {return;}
    pending.push((async () => {
      const headers = await response.allHeaders();
      assert.equal(await response.finished(), null, "NEOCD_RANGE_RESPONSE_INCOMPLETE");
      requests.push({url: response.url(), range: request.headers().range, status: response.status(),
        contentRange: headers["content-range"], sizeBytes: Number(headers["content-length"])});
    })());
  });
  return {
    async snapshot(url) {
      await Promise.all(pending);
      const entries = requests.filter(request => request.url === url);
      return {requests: entries.length, bytes: entries.reduce((sum, request) => sum + request.sizeBytes, 0)};
    },
    async verify(url, sizeBytes) {
      await Promise.all(pending);
      const entries = requests.filter(request => request.url === url);
      assert.ok(entries.length > 0, "NEOCD_RANGE_REQUESTS_MISSING");
      for (const entry of entries) {
        assert.equal(entry.status, 206); assert.ok(entry.sizeBytes > 0 && entry.sizeBytes <= 256 * 1024);
        const match = /^bytes (\d+)-(\d+)\/(\d+)$/.exec(entry.contentRange);
        assert.ok(match); assert.equal(Number(match[3]), sizeBytes);
        assert.equal(entry.range, `bytes=${match[1]}-${match[2]}`);
        assert.equal(Number(match[2]) - Number(match[1]) + 1, entry.sizeBytes);
      }
      assert.equal(new Set(entries.map(entry => entry.range)).size, entries.length, "NEOCD_RANGE_CACHE_NOT_REUSED");
      const bytes = entries.reduce((sum, entry) => sum + entry.sizeBytes, 0);
      if (sizeBytes > 32 * 1024 * 1024) {assert.ok(bytes < sizeBytes / 2, "NEOCD_RANGE_DOWNLOADED_TOO_MUCH");}
      return {requests: entries.length, bytes, ranges: entries.map(({url: _url, ...entry}) => entry)};
    },
  };
}
