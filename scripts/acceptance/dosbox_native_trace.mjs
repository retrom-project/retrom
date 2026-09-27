import assert from "node:assert/strict";
import {proofDigest} from "./content_io_case_proof.mjs";

export async function dosOracle(opened) {
  // Independent APIRequestContext oracle retrieval is outside the browser Player
  // traffic observer. The player remains RANGE_REQUIRED throughout the run.
  const response = await opened.context.request.get(opened.source.url);
  assert.equal(response.status(), 200); const bytes = await response.body();
  assert.equal(bytes.length, opened.source.sizeBytes);
  return {bytes, receipt: {scope: "TEST_ORACLE_ONLY", sizeBytes: bytes.length, sha256: proofDigest(bytes)}};
}

export async function traceDOS(opened, oracle) {
  const before = opened.network.requests.length;
  const metadata = await opened.frame.evaluate(() => {
    const emulator = globalThis.EJS_emulator, fs = emulator.Module.FS;
    return {names: fs.readdir("/"), sizeBytes: fs.stat("/" + emulator.fileName).size};
  });
  assert.ok(metadata.names.includes("game.zip")); assert.equal(metadata.sizeBytes, oracle.length);
  assert.equal(opened.network.requests.length, before, "DOS_METADATA_FETCHED_BODY");
  const trace = await opened.frame.evaluate(async encoded => {
    const oracle = Uint8Array.from(atob(encoded), character => character.charCodeAt(0));
    let seed = 0x444f5332, totalBytes = 0, boundaryReads = 0, emptyReads = 0;
    const random = () => {seed ^= seed << 13; seed ^= seed >>> 17; seed ^= seed << 5; return seed >>> 0;};
    const sizes = [0, 1, 17, 4095, 65537, 262144], started = performance.now();
    for (let index = 0; index < 10000; index++) {
      if (performance.now() - started > 110000) throw Error("DOS_TRACE_DEADLINE");
      const length = sizes[index % sizes.length];
      const offset = index % 19 === 0 ? oracle.length : index % 11 === 0 ? Math.min(oracle.length, 262144 - 13) : random() % oracle.length;
      const expected = oracle.subarray(offset, offset + length), actual = await globalThis.__dosNativeRead(offset, length);
      if (actual.code !== 0 || actual.copied !== expected.length || !actual.bytes.every((byte, n) => byte === expected[n])) {
        throw Error(`DOS_TRACE_MISMATCH:${index}:${offset}:${length}`);
      }
      if (!expected.length) emptyReads++;
      if (Math.floor(offset / 262144) !== Math.floor((offset + expected.length) / 262144)) boundaryReads++;
      totalBytes += actual.copied;
    }
    return {operations: 10000, seed: "0x444f5332", totalBytes, emptyReads, boundaryReads, elapsedMs: performance.now() - started};
  }, oracle.toString("base64"));
  assert.equal(trace.operations, 10000); assert.ok(trace.emptyReads > 0 && trace.boundaryReads > 0);
  return {metadata: {sizeBytes: metadata.sizeBytes, mounted: "game.zip", bodyRequests: 0}, trace};
}

export async function evictDOSMemory(opened, worker) {
  await opened.page.waitForFunction(() => globalThis.__dosContentAcceptance.range.quietFor(200));
  const l1 = await opened.page.evaluate(() => {
    const cache = globalThis.__dosContentAcceptance.range.reader.pool.cache, before = cache.byteLength;
    cache.clear(); return {before, after: cache.byteLength};
  });
  const l2 = await worker.evaluate(`(() => {
    const service = globalThis.__dosObservedService, before = service.pool.cache.byteLength;
    if (service.pool.stats.active || service.pool.stats.queued) throw Error("DOS_EVICTION_NOT_IDLE");
    service.pool.cache.clear(); return {before, after: service.pool.cache.byteLength, backendLeases: service.store.stats.leases};
  })()`);
  assert.equal(l1.after, 0); assert.equal(l2.after, 0);
  return {operation: "EVICT_REAL_MEMORY_CACHE", l1, l2};
}
