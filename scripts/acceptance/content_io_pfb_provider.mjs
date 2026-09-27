import assert from "node:assert/strict";
import {readFile, realpath} from "node:fs/promises";
import {resolve, join} from "node:path";
import {proofDigest} from "./content_io_case_proof.mjs";

// Freeze only the selected Target's real Provider files; game capabilities remain on the live Host.
export async function readPFBProvider(root, providerId, targetId, {nativeBaseline = "installed"} = {}) {
  assert.ok(["installed", "candidate"].includes(nativeBaseline));
  const providers = join(root, ".pfb/workspace/providers");
  const active = JSON.parse(await readFile(join(providers, "active.json"), "utf8"));
  const installed = active.providers.find(row => row.providerId === providerId); assert.ok(installed, "CONTENT_IO_PROVIDER_NOT_INSTALLED");
  const base = join(providers, "installed", providerId, installed.bundleSha256);
  assert.equal(await realpath(base), base);
  const declaration = JSON.parse(await readFile(join(base, "provider.json"), "utf8"));
  const target = declaration.targets.find(row => row.id === targetId); assert.ok(target, "CONTENT_IO_TARGET_NOT_INSTALLED");
  const integrity = JSON.parse(await readFile(join(base, "integrity.json"), "utf8"));
  const developmentBytes = await readFile(join(providers, "dev/dev-provider.json"));
  const development = JSON.parse(developmentBytes);
  assert.equal(development.providerId, providerId, "CONTENT_IO_WRONG_DEVELOPMENT_PROVIDER");
  assert.equal(development.baseBundleSha256, installed.bundleSha256, "CONTENT_IO_DEVELOPMENT_BASE_MISMATCH");
  const files = {baseline: new Map(), candidate: new Map()};
  for (const path of new Set(["client.mjs", "assets/content-io/worker.mjs", ...target.assetPaths])) {
    assert.ok(!path.includes("..") && !path.startsWith("/"), "CONTENT_IO_PROVIDER_FILE_ESCAPE");
    const expected = integrity.files.find(row => row.path === path); assert.ok(expected, "CONTENT_IO_PROVIDER_FILE_MISSING");
    const filename = resolve(base, path); assert.equal(await realpath(filename), filename);
    const bytes = await readFile(filename);
    assert.equal(proofDigest(bytes), expected.sha256); assert.equal(bytes.length, expected.sizeBytes);
    files.baseline.set(path, bytes);
    const replacements = development.files.filter(row => row.path === path); assert.ok(replacements.length <= 1);
    const replacement = replacements[0];
    if (replacement) {
      const bytes = Buffer.from(replacement.contentBase64, "base64");
      assert.equal(proofDigest(bytes), replacement.sha256); assert.equal(bytes.length, replacement.sizeBytes);
      files.candidate.set(path, bytes);
    } else files.candidate.set(path, bytes);
  }
  assert.equal(proofDigest(files.baseline.get("client.mjs")), installed.moduleSha256);
  const identity = variant => ({bundleSha256: installed.bundleSha256,
    moduleSha256: proofDigest(files[variant].get("client.mjs")), workerSha256: proofDigest(files[variant].get("assets/content-io/worker.mjs"))});
  const identities = {baseline: identity("baseline"), candidate: identity("candidate")};
  assert.notEqual(identities.baseline.moduleSha256, identities.candidate.moduleSha256, "CONTENT_IO_BASELINE_IS_CANDIDATE");
  const native = variant => [...files[variant]].filter(([path]) => path !== "client.mjs" && !path.startsWith("assets/content-io/"))
    .map(([path, bytes]) => ({path, sha256: proofDigest(bytes), sizeBytes: bytes.length}));
  const installedNativeAssets = native("baseline");
  // A necessary core fix is held constant in both adapter measurements. This
  // is explicitly a shared candidate core comparison, not a released bundle.
  if (nativeBaseline === "candidate") for (const {path} of installedNativeAssets) {
    files.baseline.set(path, files.candidate.get(path));
  }
  assert.deepEqual(native("baseline"), native("candidate"), "CONTENT_IO_PERFORMANCE_NATIVE_CORE_CHANGED");
  return {providerId, targetId, files, identities, developmentSha256: proofDigest(developmentBytes),
    nativeAssets: native("candidate"), nativeBaseline, installedNativeAssets};
}

export async function selectFrozenProvider(context, base, provider, variant) {
  assert.ok(["baseline", "candidate"].includes(variant));
  const observed = [], prefix = `/runtime/providers/${provider.providerId}/${provider.identities[variant].bundleSha256}/`;
  await context.route("**/runtime/launches/*/config", async route => {
    const response = await route.fetch(), config = await response.json();
    assert.equal(config.runtime.providerId, provider.providerId); assert.equal(config.runtime.targetId, provider.targetId);
    assert.equal(config.runtime.moduleSha256, provider.identities.candidate.moduleSha256, "CONTENT_IO_LIVE_MODULE_CHANGED");
    config.runtime.moduleSha256 = provider.identities[variant].moduleSha256;
    await route.fulfill({response, json: config});
  });
  await context.route(new URL(prefix, base).href + "**", async route => {
    const path = decodeURIComponent(new URL(route.request().url()).pathname.slice(prefix.length));
    const bytes = provider.files[variant].get(path); assert.ok(bytes, `CONTENT_IO_UNFROZEN_PROVIDER_FILE:${path}`);
    observed.push({path, sha256: proofDigest(bytes), sizeBytes: bytes.length});
    const contentType = path.endsWith(".wasm") ? "application/wasm" : /\.(?:mjs|js)$/u.test(path) ? "text/javascript; charset=utf-8" :
      path.endsWith(".html") ? "text/html" : path.endsWith(".css") ? "text/css" : path.endsWith(".png") ? "image/png" : "application/octet-stream";
    await route.fulfill({status: 200, body: bytes, contentType, headers: {
      "Cross-Origin-Resource-Policy": "same-origin", "Cross-Origin-Embedder-Policy": "require-corp",
      "Cross-Origin-Opener-Policy": "same-origin", "Cache-Control": "no-store",
    }});
  });
  return observed;
}
