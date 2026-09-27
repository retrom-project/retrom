import assert from "node:assert/strict";
import {evictDOSMemory} from "./dosbox_native_trace.mjs";
import {exitContentIOPlayer} from "./content_io_player_exit.mjs";

export async function holdDOSRange(opened) {
  let resolve, release, routeValue;
  const held = new Promise(done => {resolve = done;});
  const barrier = new Promise(done => {release = done;});
  const handler = async route => {
    if (routeValue) {await route.continue(); return;}
    routeValue = route; resolve(route); await barrier;
  };
  await opened.context.route(opened.source.url, handler);
  return {held, async continue() {await routeValue?.continue().catch(() => {}); release();}, async close() {
    await routeValue?.abort().catch(() => {}); release(); await opened.context.unroute(opened.source.url, handler);
  }};
}

export async function concurrentDOS(opened, worker, oracle) {
  const eviction = await evictDOSMemory(opened, worker), offset = 17, length = 4096;
  const held = await holdDOSRange(opened), before = opened.network.requests.length;
  try {
    await opened.page.evaluate(({offset, length}) => {
      const controller = new AbortController(), reader = globalThis.__dosContentAcceptance.range.reader;
      globalThis.__dosCancelled = reader.readInto(offset, new Uint8Array(length), controller.signal).then(() => "SUCCESS", error => error.code);
      globalThis.__dosCancellation = controller;
    }, {offset, length});
    await held.held;
    const reads = opened.frame.evaluate(async ({offset, length}) => {
      const values = await Promise.all([globalThis.__dosNativeRead(offset, length), globalThis.__dosNativeRead(offset + 3, length - 3)]);
      if (values[0].bytes.buffer === values[1].bytes.buffer) throw Error("DOS_CONCURRENT_DESTINATIONS_SHARED");
      return values.map(value => ({...value, bytes: Array.from(value.bytes)}));
    }, {offset, length});
    await opened.page.waitForTimeout(50);
    await opened.page.evaluate(() => globalThis.__dosCancellation.abort()); await held.continue();
    const cancelled = await opened.page.evaluate(() => globalThis.__dosCancelled), values = await reads;
    assert.equal(cancelled, "CONTENT_IO_ABORTED");
    for (const [index, value] of values.entries()) {
      assert.equal(value.code, 0); assert.deepEqual(Buffer.from(value.bytes), oracle.subarray(offset + index * 3, offset + length));
    }
    await opened.network.flush();
    assert.equal(opened.network.requests.length - before, 1, "DOS_OVERLAPPING_TRANSPORT_DUPLICATED");
    return {eviction, cancelled, callers: 3, nativeCallers: 2, independentCopies: true, requests: 1};
  } finally {await held.close();}
}

export async function faultDOS(opened, worker, scenario, base) {
  const eviction = await evictDOSMemory(opened, worker);
  const primed = await opened.frame.evaluate(async () => {
    const value = await globalThis.__dosNativeRead(0, 1); return {code: value.code, copied: value.copied};
  });
  assert.deepEqual(primed, {code: 0, copied: 1});
  await opened.network.flush(); const before = opened.network.requests.length;
  const held = await holdDOSRange(opened);
  let receive;
  const retained = new Promise(resolve => {receive = resolve;});
  await opened.page.exposeBinding("__dosRetainReadResult", (_source, value) => receive({value}));
  const pending = opened.frame.evaluate(async () => {
    let result;
    try {
      const value = await globalThis.__dosNativeRead(524288 + 17, 4096);
      result = {code: value.code, copied: value.copied};
    } catch (error) {result = {error: error.code ?? error.message};}
    // Retain the actual native result in Node before the exit unmounts its iframe.
    result.clientResources = globalThis.parent.__dosContentAcceptance.resources();
    void globalThis.__dosRetainReadResult(result); return result;
  }).then(value => ({value}), error => ({detached: error.message.split("\n")[0]}));
  try {
    const route = await held.held, requestRange = route.request().headers().range;
    assert.ok(requestRange); const started = performance.now();
    let injected;
    if (scenario === "read-exit") {
      await exitContentIOPlayer(opened.page, base, opened.launch); await held.continue(); injected = {kind: "EXIT_DURING_READ"};
    } else if (scenario === "worker-termination") {
      const terminated = await worker.terminate(); assert.equal(terminated.success, true);
      injected = {kind: "TERMINATE_CONTENT_WORKER", success: terminated.success};
    } else {
      const actual = await route.fetch(); assert.equal(actual.status(), 206);
      const headers = {...actual.headers()}, bytes = await actual.body();
      if (scenario === "fault-range-200") {
        const wholeHeaders = {...route.request().headers()}; delete wholeHeaders.range;
        const whole = await route.fetch({headers: wholeHeaders}); assert.equal(whole.status(), 200);
        const body = await whole.body(); assert.equal(body.length, opened.source.sizeBytes);
        delete headers["content-range"]; headers["content-length"] = String(body.length);
        await route.fulfill({status: 200, headers, body}); injected = {status: 200, bytes: body.length};
      } else if (scenario === "fault-identity-412") {
        delete headers["content-range"]; headers["content-length"] = "0";
        await route.fulfill({status: 412, headers, body: ""}); injected = {status: 412, bytes: 0};
      } else {
        const shortened = bytes.subarray(0, bytes.length - 1); headers["content-length"] = String(shortened.length);
        await route.fulfill({status: 206, headers, body: shortened}); injected = {status: 206, bytes: shortened.length, originalBytes: bytes.length};
      }
    }
    let timer;
    const settled = scenario === "read-exit" ? await Promise.race([retained, new Promise((_, reject) => {
      timer = setTimeout(() => reject(Error("DOS_EXIT_NATIVE_RESULT_NOT_OBSERVED")), 5000);
    })]).finally(() => clearTimeout(timer)) : await pending;
    assert.ok(settled.value && (settled.value.code === 29 || settled.value.error === "CONTENT_IO_ABORTED"), JSON.stringify(settled));
    assert.ok((settled.value.copied ?? 0) === 0, "DOS_NATIVE_PARTIAL_SUCCESS");
    assert.ok(performance.now() - started < 30000, "DOS_NATIVE_FAULT_HUNG");
    await held.close();
    if (scenario.startsWith("fault-")) assert.equal(opened.network.requests.length - before, 1, "DOS_FAULT_RETRIED_OR_FELL_BACK");
    return {eviction, primed, injected, settled, elapsedMs: performance.now() - started, requestRange,
      bodyRequests: opened.network.requests.length - before};
  } finally {await held.close();}
}
