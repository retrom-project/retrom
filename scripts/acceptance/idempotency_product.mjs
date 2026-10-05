import assert from "node:assert/strict";
import {randomUUID, createHash} from "node:crypto";
import {readFile, mkdir, writeFile} from "node:fs/promises";
import {resolve, join} from "node:path";
import {setTimeout as delay} from "node:timers/promises";
import {chromium} from "../../web/node_modules/@playwright/test/index.mjs";
import {fantasyClient} from "./fantasy_product_client.mjs";
import {replayable as replayCommand, finalizeUpload} from "./idempotency_replay.mjs";
import {reviewForImport} from "./rpgmaker_security_upload.mjs";

const env = process.env, base = env.RETROM_ACCEPTANCE_BASE_URL;
const output = resolve(env.RETROM_ACCEPTANCE_CASE_DIR ?? ".pfb/workspace/idempotency-product");
await mkdir(join(output,"screenshots"), {recursive: true});
const report = {caseId: "ACC-IDEM-001", status: "FAIL", operations: [], errors: []};
let browser;
try {
  assert.ok(base && env.RETROM_CHROME_EXECUTABLE && env.RETROM_IDEMPOTENCY_ROM, "IDEMPOTENCY_INPUT_REQUIRED");
  const spec = JSON.parse(await readFile(".pfb/spec.json", "utf8"));
  assert.equal(new URL(base).hostname, `${spec.id}.localhost`, "IDEMPOTENCY_PFB_REQUIRED");
  const bytes = await readFile(env.RETROM_IDEMPOTENCY_ROM);
  assert.ok(bytes.length > 16 && bytes.length <= 8 * 1024 * 1024, "IDEMPOTENCY_NES_SINGLE_PART_REQUIRED");
  report.sampleSha256 = createHash("sha256").update(bytes).digest("hex");
  browser = await chromium.launch({executablePath: env.RETROM_CHROME_EXECUTABLE, headless: true});
  const context = await browser.newContext({viewport: {width: 1280, height: 900}});
  context.setDefaultTimeout(15000);
  const client = await fantasyClient(context, base);
  await client.json("POST", "/api/v1/admin/platform-instances/recommendations/apply", {
    headers: client.writeHeaders(), data: {}, expected: 200,
  });
  const platforms = await client.json("GET", "/api/v1/admin/platform-instances?platformId=nes&limit=100");
  const platform = platforms.items.find(item => item.enabled && item.defaultCoreId === "fceumm");
  assert.ok(platform, "IDEMPOTENCY_PLATFORM_REQUIRED");
  await verifyTag(client);
  const uploaded = await replayable(client, "POST", "/api/v1/admin/uploads", {
    purpose: "GENERAL", sourceType: "FILES", files: [{clientFileId: "rom", relativePath: "acceptance.nes", sizeBytes: bytes.length}],
  }, 201);
  const upload = uploaded.value;
  const part = await client.raw("PUT", `/api/v1/admin/uploads/${upload.uploadId}/files/${upload.files[0].fileId}/parts/0`, {
    headers: {...client.writeHeaders(), "Content-Type": "application/octet-stream", "Content-Range": `bytes 0-${bytes.length - 1}/${bytes.length}`,
      "Content-Digest": `sha-256=:${createHash("sha256").update(bytes).digest("base64")}:`}, data: bytes,
  });
  assert.equal(part.status(), 204, "IDEMPOTENCY_UPLOAD_PART");
  const completed = await finalizeUpload(client,report,upload.uploadId);
  await waitJob(client, completed.value.jobId);
  await completed.replay();
  const imported = await replayable(client, "POST", "/api/v1/admin/imports", {
    uploadId: upload.uploadId, targetPlatformInstanceId: platform.id, metadataProvider: "NONE", contentMode: "STANDARD", tagIds: [],
  }, 202);
  const review = await reviewForImport(client, imported.value.importJobId);
  await imported.replay();
  const detail = await client.raw("GET", `/api/v1/admin/reviews/${review.itemId}`);
  const scraped = await replayable(client, "POST", `/api/v1/admin/reviews/${review.itemId}/scrape-candidates`, {metadataProvider: "NONE"}, 201, {"If-Match": detail.headers().etag});
  assert.equal(scraped.value.state, "SUCCEEDED", "IDEMPOTENCY_NOOP_SCRAPE_STATE");
  const preview = await replayable(client, "POST", `/api/v1/admin/reviews/${review.itemId}/previews`, {
    clientCapabilities: {secureContext: true, crossOriginIsolated: true, sharedArrayBuffer: true},
  }, 201);
  await verifyPreview(context, preview.value.playUrl);
  const current = await client.raw("GET", `/api/v1/admin/reviews/${review.itemId}`);
  const approved = await replayable(client, "POST", `/api/v1/admin/reviews/${review.itemId}/approve`, {}, 201, {"If-Match": current.headers().etag});
  assert.equal(approved.value.status, "PUBLISHED");
  const game = await client.json("GET", `/api/v1/games/${approved.value.gameId}`);
  assert.equal(game.gameId ?? game.id, approved.value.gameId);
  report.gameId = approved.value.gameId;
  report.status = "PASS";
} catch (error) {
 report.errorCode = error.message;
 if (error.message.includes("IDEMPOTENCY_INPUT_REQUIRED")) {report.status="BLOCKED";process.exitCode=3;}
 else {process.exitCode=1;}
}
finally {
  await browser?.close();
  await writeFile(join(output, "idempotency-product.json"), JSON.stringify(report, null, 2) + "\n");
  console.log(JSON.stringify({status: report.status, errorCode: report.errorCode, operations: report.operations.length}));
}

async function verifyTag(client) {
  const name = `Idempotency ${randomUUID().slice(0, 8)}`;
  const tag = await replayable(client, "POST", "/api/v1/admin/tags", {name}, 201);
  const responses = await Promise.all(Array.from({length: 4}, tag.replay));
  assert.equal(responses.length, 4);
  const conflict = await client.raw("POST", "/api/v1/admin/tags", {...tag.options, data: {name: `${name} other`}});
  assert.equal(conflict.status(), 409);
  assert.equal((await conflict.json()).error.code, "IDEMPOTENCY_KEY_REUSED");
  const renamed = await replayable(client, "PATCH", `/api/v1/admin/tags/${tag.value.tagId}`, {name: `${name} edit`}, 200,
    {"If-Match": tag.original.headers().etag});
  await replayable(client, "DELETE", `/api/v1/admin/tags/${tag.value.tagId}`, {confirmName: `${name} edit`}, 204,
    {"If-Match": renamed.original.headers().etag});
  await renamed.replay();
}

async function waitJob(client, id) {
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    const job = await client.json("GET", `/api/v1/admin/jobs/${id}`);
    if (job.state === "SUCCEEDED") {return;}
    assert.ok(!["FAILED", "CANCELLED"].includes(job.state), "IDEMPOTENCY_FINALIZATION_FAILED");
    await delay(100);
  }
  throw Error("IDEMPOTENCY_FINALIZATION_TIMEOUT");
}

async function verifyPreview(context, playUrl) {
  const page = await context.newPage();
  page.on("pageerror", error => report.errors.push(error.message.slice(0, 100)));
  await page.goto(`${base}${playUrl}`, {waitUntil: "domcontentloaded", timeout: 60000});
  const deadline = Date.now() + 60000;
  while (Date.now() < deadline) {
    for (const frame of page.frames()) {
      const canvas = frame.locator("canvas").first();
      if (await canvas.isVisible()) {
        const image = await canvas.screenshot({path: join(output, "screenshots", "review-preview.png")});
        assert.ok(image.length > 1000, "IDEMPOTENCY_PREVIEW_EMPTY");
        assert.equal(report.errors.length, 0, "IDEMPOTENCY_PREVIEW_ERRORS");
        await page.close(); return;
      }
    }
    await delay(100);
  }
  throw Error("IDEMPOTENCY_PREVIEW_TIMEOUT");
}

function replayable(client,method,path,data,status,headers) {
 return replayCommand(client,report,method,path,data,status,headers);
}
