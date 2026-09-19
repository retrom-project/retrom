import assert from "node:assert/strict";
import {contentSourceMatcher, observeContentIO, rangeSummary} from "./content_io_observation.mjs";

// This observer covers the entire context, including the Content I/O Worker.
// Register from each launch's own Envelope before navigating its Player.
export async function trackMkxpLoading(context, config, baseURL) {
  if (!["rpgmaker-xp", "rpgmaker-vx", "rpgmaker-vx-ace"].includes(config.runtime.targetId)) return null;
  const sources = config.resources.filter(source => source.kind === "SEEKABLE_BLOB");
  assert.equal(sources.length, 1, "MKXP_LOADING_SOURCE_INVALID");
  const runtimeBase = new URL(config.runtime.runtimeBaseUrl, baseURL);
  const assets = ["mkxp-z_libretro.js", "mkxp-z_libretro.wasm"].map(file => ({
    url: new URL(`assets/mkxp/${file}`, runtimeBase).href,
  }));
  const acceptsAsset = contentSourceMatcher(assets, baseURL);
  const acceptsSource = contentSourceMatcher(sources, baseURL);
  const observer = observeContentIO(context, url => acceptsAsset(url) || acceptsSource(url));
  await observer.ready;
  return {
    assetIdentity: `${runtimeBase.href}:${config.runtime.bundleSha256}`,
    async snapshot() {
      await observer.flush();
      const range = rangeSummary(observer.requests, sources[0]);
      const assetPaths = new Set(assets.map(asset => new URL(asset.url).pathname));
      const requests = observer.requests.filter(item => assetPaths.has(item.path) && item.method !== "HEAD");
      for (const request of requests) {
        assert.equal(request.failure, null, "MKXP_LOADING_CORE_TRANSFER_FAILED");
        assert.equal(request.status, 200, "MKXP_LOADING_CORE_RESPONSE_INVALID");
        assert.ok(request.sizeBytes > 0, "MKXP_LOADING_CORE_LENGTH_UNKNOWN");
      }
      return {rangeRequests: range.requests, downloadedBytes: range.downloadedBytes,
        coreAssetRequests: requests.length, fetchPolicy: range.fetchPolicy};
    },
    stop: () => observer.close(),
  };
}
