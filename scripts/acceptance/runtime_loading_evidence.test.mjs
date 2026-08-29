import assert from "node:assert/strict";
import test from "node:test";

import { summarizeRuntimeLoading } from "./runtime_loading_evidence.mjs";

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
      { contentLength: 240000, status: 200, url: "https://retrom.example/runtime/retrom-runtime/v/core.js" },
    ],
    timings: [
      { decodedBodySize: 240000, transferSize: 0, url: "https://retrom.example/runtime/retrom-runtime/v/core.js" },
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
