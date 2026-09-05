import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import test from "node:test";
import {inspectPreviewCheckpoint} from "./rpgmaker_preview_actions.mjs";

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
