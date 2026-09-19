import assert from "node:assert/strict";

// Observe the old adapter's own successful digest operation, without retaining payloads
// or replacing its verification result. Each expected object has an explicit identity.
export async function observeVerifiedDeliveries(context, sources) {
  assert.equal(new Set(sources.map(row => row.id)).size, sources.length);
  assert.equal(new Set(sources.map(row => row.sha256)).size, sources.length);
  await context.addInitScript(sources => {
    if (!crypto.subtle) return;
    const digest = crypto.subtle.digest.bind(crypto.subtle);
    const records = []; globalThis.__retromVerifiedDeliveries = records;
    crypto.subtle.digest = async (algorithm, input) => {
      const result = await digest(algorithm, input);
      const matches = sources.filter(row => row.sizeBytes === input.byteLength);
      if (matches.length) {
        const actual = Array.from(new Uint8Array(result), value => value.toString(16).padStart(2, "0")).join("");
        for (const source of matches) if (source.sha256 === actual) records.push({id: source.id, sha256: actual, sizeBytes: input.byteLength});
      }
      return result;
    };
  }, sources);
}
export async function readVerifiedDeliveries(page, sources) {
  const observed = (await Promise.all(page.frames().map(frame =>
    frame.evaluate(() => globalThis.__retromVerifiedDeliveries ?? [])))).flat();
  const expected = sources.map(({id, sha256, sizeBytes}) => ({id, sha256, sizeBytes}));
  const ordered = values => [...values].sort((a, b) => a.id < b.id ? -1 : a.id > b.id ? 1 : 0);
  assert.deepEqual(ordered(observed), ordered(expected), "CONTENT_IO_VERIFIED_DELIVERY_MISSING_OR_REPEATED");
  return observed;
}
export function verifiedFetchMetrics(requests, deliveries, sources, {cacheHits = false} = {}) {
  const expected = new Map(sources.map(row => [new URL(row.url).pathname, row]));
  const fetches = requests.filter(row => row.resourceType === "fetch");
  if (cacheHits) assert.ok(fetches.length <= sources.length, "CONTENT_IO_BASELINE_FETCH_COUNT");
  else assert.equal(fetches.length, sources.length, "CONTENT_IO_BASELINE_FETCH_COUNT");
  assert.equal(new Set(fetches.map(row => row.path)).size, fetches.length, "CONTENT_IO_BASELINE_FETCH_REPEATED");
  for (const row of requests) {
    assert.ok(expected.has(row.path)); assert.ok(["fetch", "script"].includes(row.resourceType), "CONTENT_IO_BASELINE_REQUEST_KIND");
    assert.equal(row.failure, null); assert.equal(row.status, 200); assert.equal(row.method, "GET"); assert.equal(row.range, null);
    assert.equal(row.sizeBytes, expected.get(row.path).sizeBytes);
  }
  assert.equal(deliveries.length, sources.length);
  for (const source of sources) assert.deepEqual(deliveries.find(row => row.id === source.id),
    {id: source.id, sha256: source.sha256, sizeBytes: source.sizeBytes});
  return {rangeRequests: 0, wholeRequests: fetches.length, headRequests: 0,
    networkBytes: fetches.reduce((sum, row) => sum + row.sizeBytes, 0),
    materializedBytes: deliveries.reduce((sum, row) => sum + row.sizeBytes, 0), publicPeak: null, closed: null};
}
