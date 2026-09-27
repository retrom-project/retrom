import test from "node:test";
import assert from "node:assert/strict";
import {mkdtemp, mkdir, writeFile, rm} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {readPFBProvider, selectFrozenProvider} from "../content_io_pfb_provider.mjs";
import {proofDigest} from "../content_io_case_proof.mjs";

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), "content-provider-")); t.after(() => rm(root, {recursive: true}));
  const bundle = "b".repeat(64), providerId = "retrom-runtime", targetId = "bbc-jsbeeb";
  const providers = join(root, ".pfb/workspace/providers"), base = join(providers, "installed", providerId, bundle);
  await mkdir(join(base, "assets/content-io"), {recursive: true}); await mkdir(join(providers, "dev"));
  const files = new Map([["client.mjs", Buffer.from("baseline")], ["assets/content-io/worker.mjs", Buffer.from("worker")],
    ["assets/core.wasm", Buffer.from("native")]]);
  const integrity = [];
  for (const [path, bytes] of files) {
    await writeFile(join(base, path), bytes); integrity.push({path, sizeBytes: bytes.length, sha256: proofDigest(bytes)});
  }
  await writeFile(join(base, "integrity.json"), JSON.stringify({files: integrity}));
  await writeFile(join(base, "provider.json"), JSON.stringify({targets: [{id: targetId, assetPaths: ["assets/core.wasm"]}]}));
  await writeFile(join(providers, "active.json"), JSON.stringify({providers: [{providerId, bundleSha256: bundle, moduleSha256: proofDigest(files.get("client.mjs"))}]}));
  const replacement = (path, text) => ({path, sizeBytes: Buffer.byteLength(text), sha256: proofDigest(text), contentBase64: Buffer.from(text).toString("base64")});
  const development = {providerId, baseBundleSha256: bundle, files: [replacement("client.mjs", "candidate")]};
  const publish = () => writeFile(join(providers, "dev/dev-provider.json"), JSON.stringify(development));
  await publish(); return {root, base, providerId, targetId, development, replacement, publish};
}

test("Content IO freezes different verified adapters on identical native assets", async t => {
  const f = await fixture(t), result = await readPFBProvider(f.root, f.providerId, f.targetId);
  assert.notEqual(result.identities.baseline.moduleSha256, result.identities.candidate.moduleSha256);
  assert.equal(result.identities.baseline.bundleSha256, result.identities.candidate.bundleSha256);
  assert.deepEqual(result.files.baseline.get("assets/core.wasm"), result.files.candidate.get("assets/core.wasm"));
  f.development.files.push(f.replacement("assets/core.wasm", "changed")); await f.publish();
  await assert.rejects(readPFBProvider(f.root, f.providerId, f.targetId), /NATIVE_CORE_CHANGED/u);
});
test("Content IO rejects changed bytes and a candidate reused as baseline", async t => {
  const f = await fixture(t); f.development.files = []; await f.publish();
  await assert.rejects(readPFBProvider(f.root, f.providerId, f.targetId), /BASELINE_IS_CANDIDATE/u);
  f.development.files = [f.replacement("client.mjs", "candidate")]; await f.publish();
  await writeFile(join(f.base, "assets/core.wasm"), "tampered");
  await assert.rejects(readPFBProvider(f.root, f.providerId, f.targetId));
});
test("Frozen Provider responses preserve the Player module content-type contract", async t => {
  const f = await fixture(t), provider = await readPFBProvider(f.root, f.providerId, f.targetId);
  const routes = [];
  await selectFrozenProvider({route: async (pattern, handler) => routes.push({pattern, handler})}, "http://localhost", provider, "baseline");
  let response;
  await routes[1].handler({request: () => ({url: () => `http://localhost/runtime/providers/retrom-runtime/${provider.identities.baseline.bundleSha256}/client.mjs`}),
    fulfill: async value => {response = value;}});
  assert.equal(response.contentType, "text/javascript; charset=utf-8");
  assert.equal(proofDigest(response.body), provider.identities.baseline.moduleSha256);
});
