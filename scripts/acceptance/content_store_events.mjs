// Observe numeric Host diagnostics and transport lifecycle events without reading resource capabilities.
export function selectedContentBackend(events) {
  return events.filter(event => ["READY", "BACKEND_READY"].includes(event.type) &&
    ["MEMORY", "OPFS", "CACHE_BLOCKS"].includes(event.backend)).at(-1)?.backend ?? null;
}

export async function observeContentStoreEvents(context, {retain = false} = {}) {
  const pages = new WeakMap();
  if (retain) await context.exposeBinding("__retromRetainContentMetrics", ({page}, snapshot) => {
    const sessions = pages.get(page) ?? {};
    sessions[snapshot.sessionId] = snapshot.metrics;
    pages.set(page, sessions);
  });
  await context.addInitScript((retain = false) => {
    const events = [];
    globalThis.__retromContentStoreEvents = events;
    const metrics = {sessions: Object.create(null), invalidValues: 0, overflow: false};
    globalThis.__retromContentIOMetrics = metrics;
    const fields = new Set(["networkBytes", "serverSentBytes", "rangeRequests", "wholeRequests", "headRequests", "memoryHitBytes", "persistentHitBytes",
      "readyBytes", "materializedBytes", "cacheBytesL1", "cacheBytesL2", "temporaryBytes", "outputCreditBytes", "syncBufferBytes",
      "inflight", "queued", "waiters", "channels", "leases", "retries", "wholeRestartCount", "cacheDegrades", "corruptBlocks",
      "firstFrameMs", "inputReadyMs", "exitMs", "materializedBytesByBytes", "materializedBytesByBlob", "materializedBytesBySink",
      "peakCacheBytesL1", "peakCacheBytesL2", "peakTemporaryBytes", "peakOutputCreditBytes", "peakSyncBufferBytes",
      "peakInflight", "peakQueued", "peakWaiters", "peakChannels", "peakLeases"]);
    const operations = new Set(["CACHE_PROBE", "CACHE_READ", "CACHE_WRITE", "CACHE_GC", "CACHE_DEGRADE", "READ", "MATERIALIZE", "CLOSE", "ABI"]);
    const record = (data, source) => {
      if (!data || typeof data !== "object") return;
      if (typeof data.sessionId !== "string" || !/^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/u.test(data.sessionId) ||
        !operations.has(data.operation) || !data.counts || typeof data.counts !== "object" || Array.isArray(data.counts)) return;
      let session = metrics.sessions[data.sessionId];
      if (!session) {
        if (Object.keys(metrics.sessions).length >= 32) {metrics.overflow = true; return;}
        session = metrics.sessions[data.sessionId] = {latest: {}, observedMax: {}, messages: 0};
      }
      if (session.source === "HOST" && source !== "HOST") return;
      session.source = source; session.messages++;
      session.lastOperation = data.operation;
      session.lastCodeNumber = Number.isSafeInteger(data.codeNumber) && data.codeNumber >= 0 && data.codeNumber <= 17 ? data.codeNumber : null;
      const cleanCounts = {};
      for (const [key, value] of Object.entries(data.counts)) {
        if (!fields.has(key)) continue;
        if (!(typeof value === "number" && Number.isFinite(value) && value >= 0 && (key.endsWith("Ms") || Number.isSafeInteger(value)))) {metrics.invalidValues++; continue;}
        cleanCounts[key] = value;
        session.latest[key] = value; session.observedMax[key] = Math.max(session.observedMax[key] ?? 0, value);
      }
      if (data.operation === "CLOSE") session.closeCounts = cleanCounts;
      if (retain) void globalThis.__retromRetainContentMetrics({sessionId: data.sessionId, metrics: session})
        .catch(() => {metrics.invalidValues++;});
    };
    globalThis.addEventListener("retrom:runtime-diagnostic", ({detail}) => {
      if (detail?.code !== "CONTENT_IO_METRICS" || typeof detail.message !== "string" || detail.message.length > 4096) return;
      try {record(JSON.parse(detail.message), "HOST");} catch { /* Invalid external event is not a measurement. */ }
    });
    globalThis.MessageChannel = new Proxy(globalThis.MessageChannel, {construct(Target, argumentsList, newTarget) {
      const pair = Reflect.construct(Target, argumentsList, newTarget);
      for (const port of [pair.port1, pair.port2]) port.addEventListener("message", ({data}) => {
        if (data?.v !== 1 || !["READY", "BACKEND_READY", "DIAGNOSTIC"].includes(data.type)) return;
        if (data.type === "DIAGNOSTIC") record(data, "TRANSPORT");
        if (events.length >= 100) return;
        events.push({type: data.type, backend: ["OPFS", "CACHE_BLOCKS", "MEMORY"].includes(data.backend) ? data.backend : null,
          operation: operations.has(data.operation) ? data.operation : null,
          codeNumber: Number.isSafeInteger(data.codeNumber) && data.codeNumber >= 0 ? data.codeNumber : null, at: performance.now()});
      });
      return pair;
    }});
  }, retain);
  return {snapshot: page => structuredClone(pages.get(page) ?? {})};
}
