import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import test from "node:test";
import {advanceFixture, inspectPreviewCheckpoint, observeOwnedFixture, observePreviewFrames, resumePreview, waitForPreviewReady} from "./rpgmaker_preview_actions.mjs";

test("resuming through ordinary UI waits for the core acknowledgement to hide the pause overlay", async () => {
  const calls = [];
  const resume = {
    isVisible: async () => true,
    click: async () => {calls.push("click");},
    waitFor: async ({state}) => {calls.push(state);},
  };
  const page = {getByRole: () => resume};
  await resumePreview(page);
  assert.deepEqual(calls, ["click", "hidden"]);
  resume.isVisible = async () => false;
  await resumePreview(page);
  assert.deepEqual(calls, ["click", "hidden"]);
});

test("owned RGSS state is read from the Provider diagnostic channel without adding game probes", async () => {
  let capture;
  let initialize;
  const page = {
    on() {},
    exposeBinding: async (_name, callback) => {capture = callback;},
    addInitScript: async (callback) => {initialize = callback;},
  };
  const observation = await observeOwnedFixture(page);
  assert.equal(typeof initialize, "function");
  capture({}, {code: "RETROM_RUNTIME_MKXP_Z", message: "RETROM_FIXTURE_STATE_V1:60:1:12:8:1"});
  assert.deepEqual(observation.last.position, {mapId: 1, playerX: 12, playerY: 8, fixtureState: 1});
  assert.ok(observation.last.receivedAtMs > 0);
  const last = observation.last;
  capture({}, {code: "UNRELATED", message: "RETROM_FIXTURE_STATE_V1:90:1:14:8:2"});
  capture({}, {code: "RETROM_RUNTIME_MKXP_Z", message: "unrelated native log"});
  assert.equal(observation.last, last);
});

const receipt = {resourceKind: "REVIEW_PREVIEW_CHECKPOINT", previewId: "preview-1",
  checkpointFormat: "fixture-v1", createdAtMs: 123};
test("a session finishing during mount reports diagnostics instead of waiting for the outer hard timeout", async () => {
  const pending = new Promise(() => {});
  const unavailable = async () => {throw new Error("Execution context was destroyed by navigation");};
  const locator = {waitFor: () => pending, allInnerTexts: unavailable, allTextContents: unavailable};
  const page = {
    getByRole: () => ({...locator, filter: () => ({...locator, first: () => locator})}),
    locator: () => locator,
    waitForResponse: async (predicate) => {
      assert.equal(predicate({request: () => ({method: () => "POST"}), url: () => "http://example.test/runtime/launches/session/finish"}), true);
      return {};
    },
    __retromConsoleDiagnostics: [{type: "error", message: "core initialization failed"}],
  };
  await assert.rejects(waitForPreviewReady(page), /core initialization failed/);
});
test("owned fixture movement uses taps shorter than one tile traversal instead of repeat-producing holds", async () => {
  const presses = [];
  const waits = [];
  const canvas = {
    isVisible: async () => true, evaluate: async () => {},
    press: async (key, options) => {presses.push({key, ...options});},
  };
  const page = {
    getByRole: () => ({isVisible: async () => false}),
    frames: () => [{locator: () => ({first: () => canvas})}],
    waitForTimeout: async (milliseconds) => {waits.push(milliseconds);},
  };
  await advanceFixture(page, ["ArrowRight", "ArrowRight"]);
  assert.deepEqual(presses, [{key: "ArrowRight", delay: 80}, {key: "ArrowRight", delay: 80}]);
  assert.deepEqual(waits, [800, 800]);
});
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

test("checkpoint evidence uses original CDP bytes when Playwright has no multipart body", async () => {
  const full = await request();
  const body = full.postDataBuffer();
  const unavailable = {...full, postDataBuffer: () => null};
  await assert.rejects(inspectPreviewCheckpoint(unavailable, 201, receipt, "preview-1"), /REQUEST_INVALID/);
  const result = await inspectPreviewCheckpoint(unavailable, 201, receipt, "preview-1", body);
  assert.equal(result.bytes.toString(), "actual checkpoint");
  assert.equal(result.screenshot.toString(), "actual image");
  assert.equal(result.requestContentLengthBytes, body.length);
  for (const changed of [body.subarray(1), Buffer.concat([body, Buffer.from("extra")]), Buffer.alloc(0)]) {
    await assert.rejects(inspectPreviewCheckpoint(unavailable, 201, receipt, "preview-1", changed), /REQUEST_INVALID/);
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
