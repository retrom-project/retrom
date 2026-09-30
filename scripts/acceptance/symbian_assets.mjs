import {proofDigest} from "./content_io_case_proof.mjs";

export function observeSymbianAssets(context, errors) {
  const assets = [], pending = [];
  let workerAssetPath;
  const listener = response => {
    const path = new URL(response.url()).pathname;
    if (!path.startsWith("/runtime/providers/retrom-runtime/") || !/(?:client\.mjs|worker\.mjs|eka2l1-runtime\.zip)$/u.test(path)) return;
    // A module Worker's network body can disappear with its CDP target. Its
    // actual executing script is verified separately before native gameplay.
    if (path.endsWith("/assets/content-io/worker.mjs")) {workerAssetPath = path;return;}
    pending.push(response.body().then(bytes => assets.push({path, sha256: proofDigest(bytes), sizeBytes: bytes.length}),
      error => errors.push(`SYMBIAN_PROVIDER_ASSET_UNAVAILABLE:${path}:${error.message}`)));
  };
  context.on("response", listener);
  return {assets, get workerAssetPath() {return workerAssetPath;}, flush: () => Promise.all(pending),
    dispose: () => context.off("response", listener)};
}
