import assert from "node:assert/strict";
import {observeFetchPolicy, validateFetchExtent} from "./content_fetch_policy.mjs";

// Callers register Envelope resources and entries from verified project indexes.
// Matching includes the origin; no credentials or query strings enter evidence.
export function contentSourceMatcher(sources, baseURL) {
  const urls = new Set(sources.map(source => new URL(source.url, baseURL).href));
  return url => urls.has(url);
}

export function observeContentIO(context, accepts) {
  const policy = observeFetchPolicy(context);
  const requests = [], records = new Map(), pending = new Set(), errors = [];
  Object.defineProperty(requests, "fetchPolicy", {get: () => policy.read()});
  const track = operation => {
    const task = Promise.resolve().then(operation).catch(error => {errors.push(error);});
    pending.add(task); void task.finally(() => pending.delete(task));
  };
  const register = request => {
    if (!accepts(request.url())) return null;
    let item = records.get(request);
    if (!item) {
      item = {path: new URL(request.url()).pathname, method: request.method(), resourceType: request.resourceType?.() ?? null, status: null, failure: null,
        range: null, ifMatch: null, contentRange: null, sizeBytes: null, etag: null, encoding: null,
        transferredBytes: null, consumedBytes: null};
      records.set(request, item); requests.push(item);
      track(async () => {
        const headers = await request.allHeaders();
        item.range = headers.range ?? null; item.ifMatch = headers["if-match"] ?? null;
      });
    }
    return item;
  };
  const onRequest = request => {register(request);};
  const onFailure = request => {
    const item = register(request);
    if (item) item.failure = request.failure()?.errorText ?? "REQUEST_FAILED";
  };
  const onResponse = response => {
    const request = response.request(), item = register(request);
    if (!item) return;
    track(async () => {
      const headers = await response.allHeaders();
      item.status = response.status(); item.contentRange = headers["content-range"] ?? null;
      item.etag = headers.etag ?? null; item.encoding = headers["content-encoding"] ?? null;
      item.sizeBytes = contentLength(headers["content-length"]);
      item.failure = await response.finished() ?? item.failure;
      if (!item.failure) {
        try {item.transferredBytes = contentLength(String((await request.sizes()).responseBodySize));}
        catch { /* Browser does not expose a reliable transfer measurement. */ }
      }
    });
  };
  context.on("request", onRequest); context.on("response", onResponse); context.on("requestfailed", onFailure);
  return {requests, ready: policy.ready, async flush({timeoutMs = 20000} = {}) {
    await policy.ready;
    assert.ok(Number.isSafeInteger(timeoutMs) && timeoutMs > 0, "CONTENT_IO_OBSERVATION_TIMEOUT_INVALID");
    let timer;
    const deadline = new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error("CONTENT_IO_OBSERVATION_TIMEOUT")), timeoutMs);
    });
    try {
      while (pending.size) await Promise.race([Promise.all([...pending]), deadline]);
      if (errors.length) throw new AggregateError(errors, "CONTENT_IO_OBSERVATION_FAILED");
    } finally {clearTimeout(timer);}
  }, close() {
    context.off("request", onRequest); context.off("response", onResponse); context.off("requestfailed", onFailure);
  }};
}

function contentLength(value) {
  return typeof value === "string" && /^(0|[1-9][0-9]*)$/u.test(value) && Number.isSafeInteger(Number(value)) ? Number(value) : null;
}

export function rangeSummary(requests, source, fetchPolicy = requests.fetchPolicy) {
  const path = new URL(source.url, "http://localhost").pathname;
  const matches = requests.filter(item => item.path === path && item.method !== "HEAD");
  let pinnedEtag = null;
  for (const item of matches) {
    assert.equal(item.failure, null, "CONTENT_IO_RANGE_TRANSFER_FAILED");
    validateFetchExtent(item, source, fetchPolicy);
    if (source.sha256) {
      assert.equal(item.etag, `"sha256-${source.sha256}"`, "CONTENT_IO_RANGE_ETAG_MISMATCH");
      assert.equal(item.ifMatch, item.etag, "CONTENT_IO_RANGE_IF_MATCH_MISMATCH");
    }
    else {
      assert.match(item.etag ?? "", /^"[^"\r\n]+"$/u, "CONTENT_IO_RANGE_ETAG_MISSING");
      if (pinnedEtag !== null) assert.equal(item.etag, pinnedEtag, "CONTENT_IO_RANGE_ETAG_CHANGED");
      if (item.ifMatch != null) assert.equal(item.ifMatch, item.etag, "CONTENT_IO_RANGE_IF_MATCH_MISMATCH");
      pinnedEtag = item.etag;
    }
    assert.ok(item.encoding == null || item.encoding === "identity", "CONTENT_IO_ENCODED_RANGE");
  }
  return {requests: matches.length, downloadedBytes: matches.reduce((sum, item) => sum + item.sizeBytes, 0), discBytes: source.sizeBytes, fetchPolicy};
}
