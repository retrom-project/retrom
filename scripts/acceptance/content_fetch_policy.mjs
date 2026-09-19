import assert from "node:assert/strict";
const probes = new WeakMap();
const blockBytes = 262144, maximumBytes = 2097152;

export function validateObservedFetchPolicy(value) {
  assert.ok(value && typeof value === "object" && !Array.isArray(value), "CONTENT_IO_FETCH_POLICY_MISSING");
  assert.deepEqual(Object.keys(value).sort(), ["networkWindowBytes", "smallFileThresholdBytes"]);
  const {smallFileThresholdBytes, networkWindowBytes} = value;
  assert.ok(Number.isSafeInteger(smallFileThresholdBytes) && smallFileThresholdBytes >= 0 && smallFileThresholdBytes <= maximumBytes);
  assert.ok(Number.isSafeInteger(networkWindowBytes) && networkWindowBytes >= blockBytes &&
    networkWindowBytes <= maximumBytes && networkWindowBytes % blockBytes === 0);
  return Object.freeze({smallFileThresholdBytes, networkWindowBytes});
}

// Observe only BOOT's numeric policy. No ports, resource URLs or capabilities
// are copied, and the original postMessage arguments and return value survive.
export function observeFetchPolicy(context) {
  if (probes.has(context)) return probes.get(context);
  const policies = [];
  const probe = {read() {
    assert.ok(policies.length > 0, "CONTENT_IO_FETCH_POLICY_UNOBSERVED");
    assert.ok(policies.every(policy => JSON.stringify(policy) === JSON.stringify(policies[0])), "CONTENT_IO_FETCH_POLICY_CHANGED");
    return policies[0];
  }};
  probe.ready = (async () => {
    await context.exposeBinding("__retromRecordFetchPolicy", (_source, value) => {
      policies.push(validateObservedFetchPolicy(value));
    });
    await context.addInitScript(() => {
      const postMessage = Worker.prototype.postMessage;
      Worker.prototype.postMessage = function (...args) {
        const message = args[0];
        if (message?.v === 1 && message.type === "BOOT") {
          void globalThis.__retromRecordFetchPolicy({
            smallFileThresholdBytes: message.fetchPolicy?.smallFileThresholdBytes,
            networkWindowBytes: message.fetchPolicy?.networkWindowBytes,
          });
        }
        return Reflect.apply(postMessage, this, args);
      };
    });
  })();
  probes.set(context, probe);
  return probe;
}

export function validateFetchExtent(item, source, policy) {
  const {smallFileThresholdBytes, networkWindowBytes} = validateObservedFetchPolicy(policy);
  assert.ok(Number.isSafeInteger(source.sizeBytes) && source.sizeBytes > 0, "CONTENT_IO_SOURCE_SIZE_INVALID");
  if (item.status === 200) {
    assert.ok(source.sizeBytes <= smallFileThresholdBytes && item.range == null, "CONTENT_IO_WHOLE_RESPONSE");
    assert.equal(item.contentRange, null, "CONTENT_IO_WHOLE_CONTENT_RANGE");
    assert.equal(item.sizeBytes, source.sizeBytes, "CONTENT_IO_WHOLE_LENGTH_MISMATCH");
    return {start: 0, end: source.sizeBytes - 1};
  }
  assert.equal(item.status, 206, "CONTENT_IO_RESPONSE_INVALID");
  const range = /^bytes=(\d+)-(\d+)$/u.exec(item.range ?? "");
  assert.ok(range, "CONTENT_IO_RANGE_MISSING");
  const start = Number(range[1]), end = Number(range[2]);
  assert.ok(Number.isSafeInteger(start) && Number.isSafeInteger(end) && start >= 0 && end >= start &&
    end < source.sizeBytes && start % blockBytes === 0 &&
    ((end + 1) % blockBytes === 0 || end + 1 === source.sizeBytes), "CONTENT_IO_RANGE_UNBOUNDED");
  const windowEnd = source.sizeBytes <= smallFileThresholdBytes ? source.sizeBytes :
    Math.min(source.sizeBytes, (Math.floor(start / networkWindowBytes) + 1) * networkWindowBytes);
  assert.ok(end < windowEnd, "CONTENT_IO_RANGE_CROSSES_WINDOW");
  assert.equal(item.sizeBytes, end - start + 1, "CONTENT_IO_RANGE_LENGTH_MISMATCH");
  assert.equal(item.contentRange, `bytes ${start}-${end}/${source.sizeBytes}`, "CONTENT_IO_RANGE_IDENTITY_MISMATCH");
  return {start, end};
}

export function assertNoRepeatedBlocks(entries, sourceKey, seen = new Set()) {
  for (const {start, end} of entries) for (let offset = start; offset <= end; offset += blockBytes) {
    const key = `${sourceKey}:${offset}`;
    assert.equal(seen.has(key), false, "CONTENT_IO_CACHED_BLOCK_REDOWNLOADED");
    seen.add(key);
  }
}
