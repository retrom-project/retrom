// Run inside the selected PFB container with its runtime source and dependencies.
// Rebuild the released adapter with the same native fix as the candidate. First
// reproduce the installed module exactly, so this cannot substitute a new adapter.
import assert from "node:assert/strict";
import {execFileSync} from "node:child_process";
import {readFile, writeFile, mkdir, mkdtemp, symlink, rm} from "node:fs/promises";
import {join, resolve} from "node:path";
import {pathToFileURL} from "node:url";
import {proofDigest} from "./content_io_case_proof.mjs";

const [runtimeRoot, outputPath] = process.argv.slice(2).map(value => resolve(value));
assert.ok(runtimeRoot && outputPath);
await mkdir(outputPath, {recursive: false});
const providers = resolve(".pfb/workspace/providers"), active = JSON.parse(await readFile(join(providers, "active.json")));
const installed = active.providers.find(row => row.providerId === "emulatorjs"); assert.ok(installed);
const base = join(providers, "installed/emulatorjs", installed.bundleSha256);
const manifest = JSON.parse(await readFile(join(base, "provider.json")));
const integrity = JSON.parse(await readFile(join(base, "integrity.json")));
const developmentBytes = await readFile(join(providers, "dev/dev-provider.json")), development = JSON.parse(developmentBytes);
assert.equal(development.providerId, "emulatorjs"); assert.equal(development.baseBundleSha256, installed.bundleSha256);
const tag = `v${manifest.providerVersion}`;
assert.match(tag, /^v\d+\.\d+\.\d+$/u);
const sourceCommit = execFileSync("git", ["-C", runtimeRoot, "rev-parse", `${tag}^{commit}`], {encoding: "utf8"}).trim();
const source = await mkdtemp(join(outputPath, "source-"));
try {
  const archive = execFileSync("git", ["-C", runtimeRoot, "archive", sourceCommit], {maxBuffer: 64 * 1024 * 1024});
  execFileSync("tar", ["-x", "-C", source], {input: archive});
  await symlink(join(runtimeRoot, "node_modules"), join(source, "node_modules"));
  const builderPath = "scripts/provider-client-build.mjs";
  const builder = await readFile(join(runtimeRoot, builderPath));
  assert.deepEqual(await readFile(join(source, builderPath)), builder, "DOS_BASELINE_BUILDER_CHANGED");
  const lock = await readFile(join(runtimeRoot, "package-lock.json"));
  assert.deepEqual(await readFile(join(source, "package-lock.json")), lock, "DOS_BASELINE_TOOLCHAIN_CHANGED");
  const {buildProviderClient} = await import(pathToFileURL(join(runtimeRoot, builderPath)).href);
  const assetIndex = Object.fromEntries(integrity.files.filter(row => row.path.startsWith("assets/"))
    .map(({path, sha256, sizeBytes}) => [path, {sha256, sizeBytes}]));
  const build = (name, pfbCoreInputs = {}) => buildProviderClient({providerVersion: manifest.providerVersion,
    entryPoint: join(source, "src/providers/emulatorjs/module.ts"), assetIndex, pfbCoreInputs, outfile: join(outputPath, name, "client.mjs")});
  await build("reproduction");
  assert.equal(proofDigest(await readFile(join(outputPath, "reproduction/client.mjs"))), installed.moduleSha256, "DOS_BASELINE_NOT_REPRODUCIBLE");
  const corePath = "assets/4.3.0-pre/data/cores/dosbox_pure-thread-wasm.data";
  const reportPath = "assets/4.3.0-pre/data/cores/reports/dosbox_pure.json";
  const coreFiles = [corePath, reportPath].map(path => {
    const file = development.files.find(row => row.path === path); assert.ok(file, `DOS_BASELINE_CORE_MISSING:${path}`);
    const bytes = Buffer.from(file.contentBase64, "base64");
    assert.equal(proofDigest(bytes), file.sha256); assert.equal(bytes.length, file.sizeBytes);
    assetIndex[path] = {sha256: file.sha256, sizeBytes: file.sizeBytes};
    return {path, sha256: file.sha256, sizeBytes: file.sizeBytes};
  });
  const [core, provenance] = coreFiles;
  await build("shared-core", {dosbox_pure: {sha256: core.sha256, sizeBytes: core.sizeBytes, artifactSetSha256: provenance.sha256}});
  const bytes = await readFile(join(outputPath, "shared-core/client.mjs"));
  await writeFile(join(outputPath, "baseline.json"), JSON.stringify({schemaVersion: 1, kind: "RELEASED_ADAPTER_SHARED_CANDIDATE_CORE",
    sourceTag: tag, sourceCommit, sourceArchiveSha256: proofDigest(archive), builderSha256: proofDigest(builder), lockSha256: proofDigest(lock),
    installedModuleSha256: installed.moduleSha256, moduleSha256: proofDigest(bytes), developmentSha256: proofDigest(developmentBytes), coreFiles}, null, 2) + "\n");
  console.log(JSON.stringify({sourceTag: tag, sourceCommit, moduleSha256: proofDigest(bytes), coreSha256: core.sha256}));
} finally {await rm(source, {recursive: true});}
