import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import test from "node:test";
import {inspectPreviewCheckpoint, observePreviewFrames} from "./rpgmaker_preview_actions.mjs";

const receipt = {resourceKind: "REVIEW_PREVIEW_CHECKPOINT", previewId: "preview-1",
  checkpointFormat: "fixture-v1", createdAtMs: 123};
async function request(includeScreenshot = true) {
  const form = new FormData();
  form.append("metadata", new Blob(["{}"], {type: "application/json"}));
  form.append("payload", new Blob(["actual checkpoint"]), "payload.bin");
  if (includeScreenshot) {form.append("screenshot", new Blob(["actual image"]), "screenshot.png");}
  const response = new Response(form);
  const body = Buffer.from(await response.arrayBuffer());
  return {
    allHeaders: async () => ({"content-type": response.headers.get("content-type"), "content-length": String(body.length)}),
    postDataBuffer: () => body,
  };
}
test("acceptance binds actual ordinary upload bytes and receipt to its preview", async () => {
  const result = await inspectPreviewCheckpoint(await request(), 201, receipt, "preview-1");
  assert.equal(result.bytes.toString(), "actual checkpoint");
  assert.equal(result.screenshot.toString(), "actual image");
  assert.equal(result.sha256, createHash("sha256").update("actual checkpoint").digest("hex"));
  assert.equal(result.sizeBytes, 17);
  assert.equal(result.format, "fixture-v1");
  assert.ok(result.requestContentLengthBytes > result.sizeBytes);
});

test("checkpoint evidence restores CDP-omitted file parts from the exact unmodified XHR FormData", async () => {
  const full = await request();
  const headers = await full.allHeaders();
  const body = full.postDataBuffer();
  const form = await new Response(body, {headers}).formData();
  let skeleton = body;
  const parts = [];
  for (const [name, value] of form) {
    const bytes = Buffer.from(await value.arrayBuffer());
    parts.push({name, fileName: value.name, type: value.type, sizeBytes: bytes.length, base64: bytes.toString("base64")});
    const offset = skeleton.indexOf(bytes);
    skeleton = Buffer.concat([skeleton.subarray(0, offset), skeleton.subarray(offset + bytes.length)]);
  }
  const url = "http://test.localhost/runtime/launches/preview-1/save-states";
  const observed = {url, method: "POST", idempotencyKey: "checkpoint-1", parts};
  const truncated = {allHeaders: async () => ({...headers, "idempotency-key": "checkpoint-1"}),
    postDataBuffer: () => skeleton, url: () => url, method: () => "POST"};
  const result = await inspectPreviewCheckpoint(truncated, 201, receipt, "preview-1", observed);
  assert.equal(result.bytes.toString(), "actual checkpoint");
  assert.equal(result.screenshot.toString(), "actual image");
  assert.equal(result.requestContentLengthBytes, body.length);
  for (const changed of [{...observed, idempotencyKey: "another"}, {...observed, url: url + "/other"},
    {...observed, parts: [...parts, parts[0]]}, {...observed, parts: parts.slice(1)},
    {...observed, parts: parts.map(part => ({...part, sizeBytes: part.sizeBytes + 1}))}]) {
    await assert.rejects(inspectPreviewCheckpoint(truncated, 201, receipt, "preview-1", changed),
      /RPG_PREVIEW_CHECKPOINT_/);
  }
});
test("failed/cross-preview uploads cannot become checkpoint evidence", async () => {
  for (const [status, value] of [[401, receipt], [201, {...receipt, previewId: "another"}],
    [201, {...receipt, resourceKind: "SAVE_STATE"}], [201, {}]]) {
    await assert.rejects(inspectPreviewCheckpoint(await request(), status, value, "preview-1"),
      /RPG_PREVIEW_CHECKPOINT_RECEIPT_INVALID/);
  }
  await assert.rejects(inspectPreviewCheckpoint(await request(false), 201, receipt, "preview-1"),
    /RPG_PREVIEW_CHECKPOINT_PAYLOAD_MISSING/);
});

test("checkpoint failure reports HTTP status and only a bounded public error code", async () => {
  for (const [body, code] of [
    [{error: {code: "INVALID_REQUEST", message: "private response details"}}, "INVALID_REQUEST"],
    [{error: {code: "credential=secret"}}, "UNKNOWN"],
    [{error: {code: "A".repeat(129)}}, "UNKNOWN"],
    [null, "UNKNOWN"],
  ]) {
    await assert.rejects(inspectPreviewCheckpoint(await request(), 400, body, "preview-1"), {
      message: "RPG_PREVIEW_CHECKPOINT_RECEIPT_INVALID:HTTP_400:" + code,
    });
  }
});

test("frame timeout retains bounded lifecycle diagnostics instead of an uncorrelated timeout", async () => {
  const page = {
    getByRole: () => ({isVisible: async () => false, allInnerTexts: async () => ["session closed"]}),
    evaluate: async () => 12,
    waitForFunction: async () => {throw new Error("timeout");},
    __retromPageErrors: ["runtime failed"],
    __retromConsoleDiagnostics: ["reload detected"],
    __retromNetworkRequests: [{method: "POST", status: 200, url: "/finish"}],
  };
  await assert.rejects(observePreviewFrames(page), (error) => {
    assert.ok(error.message.startsWith("RPG_PREVIEW_FRAMES_STALLED:"));
    const details = JSON.parse(error.message.split(":").slice(1).join(":"));
    assert.equal(details.beforeFrame, 12);
    assert.equal(details.afterFrame, 12);
    assert.deepEqual(details.pageErrors, ["runtime failed"]);
    assert.deepEqual(details.networkRequests, page.__retromNetworkRequests);
    return true;
  });
});
