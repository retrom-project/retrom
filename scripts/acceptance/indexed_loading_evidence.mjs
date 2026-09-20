import {observeFetchPolicy} from "./content_fetch_policy.mjs";
import {createHash} from "node:crypto";
import assert from "node:assert/strict";
import {observeContentIO, contentSourceMatcher, rangeSummary} from "./content_io_observation.mjs";
import {trackRuntimeLoading} from "./runtime_loading_evidence.mjs";

export async function indexedLoading(context, launchId, baseURL, core) {
  await observeFetchPolicy(context).ready;
  const configResponse = await context.request.get(`${baseURL}/runtime/launches/${launchId}/config`);
  assert.equal(configResponse.status(), 200, "INDEXED_LOADING_CONFIG_FAILED");
  const config = await configResponse.json();
  const trees = config.resources.filter(source => source.kind === "FILE_TREE");
  assert.equal(trees.length, 1, "INDEXED_LOADING_TREE_INVALID");
  const indexURL = new URL(trees[0].indexUrl, baseURL).href;
  const indexResponse = await context.request.get(indexURL);
  assert.equal(indexResponse.status(), 200, "INDEXED_LOADING_INDEX_FAILED");
  const index = await indexResponse.json();
  assert.ok(Array.isArray(index.files) && index.files.length > 0, "INDEXED_LOADING_FILES_INVALID");
  const files = index.files.map(file => ({url: new URL(file.url, indexURL).href, sizeBytes: file.sizeBytes}));
  assert.ok(files.every(file => Number.isSafeInteger(file.sizeBytes) && file.sizeBytes > 0), "INDEXED_LOADING_SIZE_INVALID");
  const assetNames = core === "kirikiri" ? ["vlfs.js", "index.js", "index.wasm", "assets.zip"] : [];
  const assets = assetNames.map(file => ({url: new URL(`assets/${core}/${file}`, new URL(config.runtime.runtimeBaseUrl, baseURL)).href}));
  return {
    assetIdentity: `${config.runtime.runtimeBaseUrl}:${config.runtime.bundleSha256}`,
    track(page) {
      const probe = trackRuntimeLoading(page, files, {collectRuntimeTimings: core !== "kirikiri", timeoutMs: 60000});
      const acceptsFile = contentSourceMatcher(files, baseURL), acceptsAsset = contentSourceMatcher(assets, baseURL);
      const observer = observeContentIO(context, url => acceptsFile(url) || acceptsAsset(url));
      return {async snapshot() {
        await observer.flush();
        const snapshot = await probe.snapshot();
        const assetPaths = new Set(assets.map(asset => new URL(asset.url).pathname));
        const coreRequests = observer.requests.filter(item => assetPaths.has(item.path) && item.method !== "HEAD");
        assert.ok(coreRequests.every(item => item.status === 200 && !item.failure && item.sizeBytes > 0), "INDEXED_LOADING_CORE_FAILED");
        if (core === "kirikiri") for (const file of files) rangeSummary(observer.requests, file);
        else for (const file of files) {
          const path = new URL(file.url).pathname;
          for (const item of observer.requests.filter(item => item.path === path && item.method !== "HEAD")) {
            assert.equal(item.failure, null, "INDEXED_LOADING_TRANSFER_FAILED");
            assert.equal(item.status, 200, "INDEXED_LOADING_WHOLE_EXPECTED");
            assert.equal(item.sizeBytes, file.sizeBytes, "INDEXED_LOADING_LENGTH_MISMATCH");
            assert.match(item.etag ?? "", /^"[^"\r\n]+"$/u, "INDEXED_LOADING_ETAG_MISSING");
          }
        }
        const rangeBlocks = observer.requests.filter(item => !assetPaths.has(item.path) && item.method !== "HEAD").map(item => ({
          sourceKey: createHash("sha256").update(item.path).digest("hex"), range: item.range, sizeBytes: item.sizeBytes,
          sourceSizeBytes: files.find(file => new URL(file.url).pathname === item.path).sizeBytes,
        }));
        return {...snapshot, fetchPolicy: core === "kirikiri" ? observer.requests.fetchPolicy : undefined, coreAssetRequests: coreRequests.length, rangeBlocks: core === "kirikiri" ? rangeBlocks : undefined};
      }, stop() {probe.stop(); observer.close();}};
    },
  };
}

export function indexedLoadingEvidence(cold, first, restored, identities) {
  const rangeRequests = cold.rangeBlocks === undefined ? null : {
    cold: cold.rangeBlocks, first: first.rangeBlocks, restored: restored.rangeBlocks,
  };
  return {schemaVersion: rangeRequests ? 4 : 2, ...(rangeRequests ? {rangeRequests,
    fetchPolicies: {cold: cold.fetchPolicy, first: first.fetchPolicy, restored: restored.fetchPolicy}} : {}),
    sameProjectContentIdentity: cold.projectContentIdentity !== null &&
      cold.projectContentIdentity === first.projectContentIdentity && first.projectContentIdentity === restored.projectContentIdentity,
    sameCoreAssetIdentity: identities.every(value => value === identities[0]),
    coldVisible: cold.evidence, firstVisible: first.evidence, restoreVisible: restored.evidence,
    coreAssetRequests: {cold: cold.coreAssetRequests, first: first.coreAssetRequests, restored: restored.coreAssetRequests}};
}
