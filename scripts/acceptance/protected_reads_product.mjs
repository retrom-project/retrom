import assert from "node:assert/strict";
import {randomUUID, createHash} from "node:crypto";
import {mkdir, readFile, writeFile} from "node:fs/promises";
import {join, relative, resolve} from "node:path";
import {setTimeout as delay} from "node:timers/promises";
import {request} from "../../web/node_modules/@playwright/test/index.mjs";
import {fantasyClient} from "./fantasy_product_client.mjs";
import {directoryFiles} from "./rpgmaker_security_upload.mjs";
import {emulatorjsSlowProxy} from "./emulatorjs_slow_proxy.mjs";

// Public upload/import/approval APIs only. Keep generated sources and evidence
// inside the selected PFB; appended identifiers preserve the public GBA code.
const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const spec = JSON.parse(await readFile(".pfb/spec.json", "utf8"));
assert.equal(new URL(base).hostname, `${spec.id}.localhost`, "READ_LOAD_PFB_REQUIRED");
const root = resolve(".pfb/protected-read-product", randomUUID());
const output = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? root);
await mkdir(root, {recursive: true}); await mkdir(output, {recursive: true});
const report = {caseId: "ACC-DB-003", status: "FAIL", fixture: "testdata/public-roms/gba-smoke/gba-smoke.gba", batches: [], samples: []};
const fixture = await readFile(report.fixture);
report.fixtureSha256 = createHash("sha256").update(fixture).digest("hex");
const proxy = await emulatorjsSlowProxy(base);
const context = await request.newContext(proxy.contextOptions), client = await fantasyClient({request: context}, base);
try {
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), expected: 200, data: {},
  });
  const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=gba&limit=100");
  const platform = platforms.items.find(item => item.enabled && item.defaultCoreId === "mgba");
  assert.ok(platform, "READ_LOAD_GBA_PLATFORM_REQUIRED");
  let initialApproval = env.RETROM_READ_LOAD_INITIAL_APPROVAL;
  if (initialApproval) {
    assert.ok((await bulk(initialApproval)).initialPendingCount >= 2272, "READ_LOAD_INITIAL_SCOPE_TOO_SMALL");
    report.resumedInitialApproval = initialApproval;
  } else {
    const first = await importBatch(2272, platform.id);
    await waitImport(first); initialApproval = await startApproval();
  }
  await sample("initial-approval", () => bulk(initialApproval));
  await waitBulk(initialApproval);
  assert.ok((await bulk(initialApproval)).publishedCount >= 2272, "READ_LOAD_NOT_PUBLISHED");
  const second = await importBatch(136, platform.id);
  await waitImport(second);
  const loadedApproval = await startApproval();
  const thirdImport = importBatch(136, platform.id);
  await sample("large-library-approval-and-upload", () => bulk(loadedApproval));
  const third = await thirdImport;
  await sample("large-library-import", () => client.json("GET", `/api/v1/admin/imports/${third.importJobId}`));
  await waitImport(third); await waitBulk(loadedApproval);
  const finalApproval = await startApproval();
  await sample("large-library-final-approval", () => bulk(finalApproval));
  await waitBulk(finalApproval);
  report.approvals = await Promise.all([initialApproval, loadedApproval, finalApproval].map(bulk));
  const timings = report.samples.map(row => row.ms).sort((a, b) => a - b);
  report.p95Ms = timings[Math.ceil(timings.length * 0.95) - 1]; report.maxMs = timings.at(-1);
  report.failures = report.samples.filter(row => row.status !== 200 || row.ms >= 500).length;
  assert.equal(report.failures, 0, "READ_LOAD_PROTECTED_GET_EXCEEDED_500MS");
  assert.ok(report.samples.some(row => row.phase.startsWith("large-library") && ["QUEUED", "RUNNING"].includes(row.workState)), "READ_LOAD_NO_ACTIVE_WORK");
  report.status = "PASS";
} catch (error) {report.error = error.message; process.exitCode = 1;}
finally {
  await context.dispose(); await proxy.close();
  await writeFile(join(output, "protected-reads-product.json"), JSON.stringify(report, null, 2) + "\n");
  console.log(JSON.stringify({status: report.status, error: report.error, p95Ms: report.p95Ms, maxMs: report.maxMs, samples: report.samples.length, evidence: relative(process.cwd(), output)}));
}

async function importBatch(count, platformId) {
  const directory = join(root, `source-${report.batches.length}`), nonce = randomUUID();
  await mkdir(directory, {recursive: true});
  for (let index = 0; index < count; index++) {
    await writeFile(join(directory, `Read load ${nonce} ${String(index).padStart(4, "0")}.gba`),
      Buffer.concat([fixture, Buffer.from(`retrom-read-load:${nonce}:${index}`)]));
  }
  let uploadId = count === 2272 ? env.RETROM_READ_LOAD_PREPARED_UPLOAD : null;
  if (uploadId) {
    const prepared = await client.json("GET", `/api/v1/admin/uploads/${uploadId}`);
    assert.equal(prepared.state, "COMPLETE"); assert.equal(prepared.files.length, count);
    assert.ok(prepared.files.every(file => file.relativePath.startsWith("Read load ") && file.state === "COMPLETE"));
  } else {uploadId = await client.upload(directoryFiles(directory), "FILES", "GENERAL");}
  const created = await client.json("POST", "/api/v1/admin/imports", {headers: client.writeHeaders(), expected: 202,
    data: {uploadId, targetPlatformInstanceId: platformId, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: []}});
  const batch = {count, uploadId, importJobId: created.importJobId}; report.batches.push(batch);
  console.log(JSON.stringify({event: "import-started", ...batch}));
  return batch;
}

async function waitImport(batch) {
  const deadline = Date.now() + 600000;
  while (Date.now() < deadline) {
    const detail = await client.json("GET", `/api/v1/admin/imports/${batch.importJobId}`);
    assert.equal(detail.counts.failed, 0, "READ_LOAD_IMPORT_FAILED");
    if (detail.counts.reviewPending === batch.count) {batch.counts = detail.counts; return;}
    await delay(200);
  }
  throw Error("READ_LOAD_IMPORT_TIMEOUT");
}

async function startApproval() {
  const created = await client.json("POST", "/api/v1/admin/review-bulk-approvals", {headers: client.writeHeaders(), expected: 202, data: {}});
  return created.bulkApprovalId;
}
function bulk(id) {return client.json("GET", `/api/v1/admin/review-bulk-approvals/${id}`);}
async function waitBulk(id) {
  const deadline = Date.now() + 600000;
  while (Date.now() < deadline) {
    const state = await bulk(id); assert.notEqual(state.state, "FAILED");
    if (state.state === "COMPLETED") {return;}
    await delay(200);
  }
  throw Error("READ_LOAD_APPROVAL_TIMEOUT");
}
async function sample(phase, work) {
  const offset = report.samples.length;
  for (const path of ["/api/v1/home", "/api/v1/games?limit=50", "/api/v1/favorites?limit=50", "/api/v1/admin/reviews?limit=20", "/api/v1/admin/users?limit=20"]) {
    for (let index = 0; index < 5; index++) {
      const state = await work();
      const started = performance.now(), response = await client.raw("GET", path);
      report.samples.push({phase, path, status: response.status(), ms: performance.now() - started,
        workState: state.state, publishedCount: state.publishedCount ?? null});
      await response.dispose();
    }
  }
  const ms = report.samples.slice(offset).map(row => row.ms).sort((a, b) => a - b);
  console.log(JSON.stringify({event: "protected-reads", phase, requests: ms.length, p95Ms: ms[Math.ceil(ms.length * 0.95) - 1], maxMs: ms.at(-1)}));
}
