import assert from "node:assert/strict";
import test from "node:test";

import { summarizeRuntimeLoading, trackRuntimeLoading } from "./runtime_loading_evidence.mjs";

test("summarizes lazy project reads without serializing logical paths", () => {
  const identity = "a".repeat(64);
  const root = `https://retrom.example/runtime/content/project/${identity}/`;
  const summary = summarizeRuntimeLoading({
    indexes: [{
      url: `${root}index.json`,
      files: [
        { url: `${root}0.txt`, sizeBytes: 2048 },
        { url: `${root}movie/unused.mp4`, sizeBytes: 12 * 1024 * 1024 },
      ],
    }],
    responses: [
      { contentLength: 800, status: 200, url: `${root}index.json` },
      { contentLength: 2048, status: 200, url: `${root}0.txt` },
      { contentLength: 240000, status: 200, url: `https://retrom.example/runtime/providers/retrom-runtime/${"c".repeat(64)}/core.js` },
    ],
    timings: [
      { decodedBodySize: 240000, transferSize: 0, url: `https://retrom.example/runtime/providers/retrom-runtime/${"c".repeat(64)}/core.js` },
    ],
  });

  assert.deepEqual(summary, {
    declaredLargeFileCount: 1,
    declaredProjectBytes: 12 * 1024 * 1024 + 2048,
    declaredProjectFileCount: 2,
    fullProjectFileResponseCount: 1,
    nativeProjectResponseCount: 0,
    projectContentIdentityCount: 1,
    rangeProjectFileResponseCount: 0,
    requestedLargeFileCount: 0,
    requestedProjectBytes: 2048,
    requestedProjectFileCount: 1,
    runtimeAssetCacheHitCount: 1,
    runtimeAssetRequestCount: 1,
    runtimeAssetTransferredBytes: 0,
  });
  assert.equal(JSON.stringify(summary).includes("unused.mp4"), false);
});

test("counts strict partial reads without treating them as full responses", () => {
  const identity = "b".repeat(64);
  const url = `https://retrom.example/runtime/content/project/${identity}/game.mkxpz`;
  const summary = summarizeRuntimeLoading({
    indexes: [{ url: `${url}/index.json`, files: [{ url, sizeBytes: 32 * 1024 * 1024 }] }],
    responses: [{ contentLength: 262144, status: 206, url }],
    timings: [],
  });
  assert.equal(summary.fullProjectFileResponseCount, 0);
  assert.equal(summary.rangeProjectFileResponseCount, 1);
  assert.equal(summary.requestedProjectBytes, 262144);
});

test("does not count HEAD metadata as transferred project bytes", () => {
  const identity = "c".repeat(64);
  const url = `https://retrom.example/runtime/content/project/${identity}/game.mkxpz`;
  const summary = summarizeRuntimeLoading({
    indexes: [{ files: [{ url, sizeBytes: 8 * 1024 * 1024 }] }],
    responses: [
      { contentLength: 8 * 1024 * 1024, method: "HEAD", status: 206, url },
      { contentLength: 262144, method: "GET", status: 206, url },
    ],
    timings: [],
  });
  assert.equal(summary.rangeProjectFileResponseCount, 1);
  assert.equal(summary.requestedProjectBytes, 262144);
});

test("does not evaluate frame resource timings when the caller disables them", async () => {
  const page = {
    frames: () => [{ evaluate: () => { throw new Error("frame timing must not run"); } }],
    off: () => {},
    on: () => {},
  };
  const probe = trackRuntimeLoading(page, [], { collectRuntimeTimings: false });
  await assert.doesNotReject(probe.snapshot());
});

test("fails a loading snapshot at its own bounded deadline", async () => {
  const page = {
    frames: () => [{ evaluate: () => new Promise(() => {}) }],
    off: () => {},
    on: () => {},
  };
  const probe = trackRuntimeLoading(page, [], { timeoutMs: 10 });
  const harnessDeadline = new Promise((_, reject) => {
    setTimeout(() => reject(new Error("TEST_BOUNDARY_EXPIRED")), 100);
  });
  await assert.rejects(Promise.race([probe.snapshot(), harnessDeadline]), /RUNTIME_LOADING_EVIDENCE_TIMEOUT/);
});

test("records native project responses without awaiting streaming headers", async () => {
  let responseListener;
  const page = {
    frames: () => [],
    off: () => {},
    on: (event, listener) => { if (event === "response") { responseListener = listener; } },
  };
  const probe = trackRuntimeLoading(page, [], { collectRuntimeTimings: false, timeoutMs: 10 });
  responseListener({
    allHeaders: () => new Promise(() => {}),
    request: () => ({ allHeaders: () => new Promise(() => {}), method: () => "GET" }),
    status: () => 200,
    url: () => "https://runtime.example/__retrom/project/data/System.json",
  });
  const snapshot = await probe.snapshot();
  assert.equal(snapshot.evidence.nativeProjectResponseCount, 1);
});
