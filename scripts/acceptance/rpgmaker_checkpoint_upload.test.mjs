import assert from "node:assert/strict";
import test from "node:test";
import {runInNewContext} from "node:vm";
import {installCheckpointUploadObservation} from "./rpgmaker_checkpoint_upload.mjs";

function fixture() {
  class Request {
    open(...args) {this.openArguments = args;}
    setRequestHeader(...args) {this.headerArguments = args;}
    send(body) {this.sentBody = body; this.sent = true; return "native-result";}
  }
  const context = {XMLHttpRequest: Request, FormData, Blob, Uint8Array, URL, btoa,
    location: {href: "http://test.localhost/play/preview-1"}};
  const original = {...{open: Request.prototype.open, send: Request.prototype.send,
    setRequestHeader: Request.prototype.setRequestHeader}};
  runInNewContext("(" + installCheckpointUploadObservation.toString() + ")('preview-1')", context);
  return {context, Request, original, observation: context.__retromCheckpointUploadObservation};
}

function checkpointForm() {
  const form = new FormData();
  form.append("metadata", new Blob(["{}"], {type: "application/json"}));
  form.append("payload", new Blob(["checkpoint"]), "payload.bin");
  form.append("screenshot", new Blob(["image"], {type: "image/png"}), "screenshot.png");
  return form;
}

test("observer reads immutable parts without delaying or replacing native XHR arguments", async () => {
  const {Request, original, context, observation} = fixture();
  const request = new Request();
  const form = checkpointForm();
  request.open("POST", "/runtime/launches/preview-1/save-states", true);
  request.setRequestHeader("Idempotency-Key", "checkpoint-1");
  assert.equal(request.send(form), "native-result");
  assert.equal(request.sent, true);
  assert.equal(request.sentBody, form);
  assert.deepEqual(request.openArguments, ["POST", "/runtime/launches/preview-1/save-states", true]);
  assert.deepEqual(request.headerArguments, ["Idempotency-Key", "checkpoint-1"]);
  form.set("payload", new Blob(["later mutation"]), "changed.bin");
  const captured = await observation.take();
  assert.equal(captured.idempotencyKey, "checkpoint-1");
  assert.equal(Buffer.from(captured.parts[1].base64, "base64").toString(), "checkpoint");
  await assert.rejects(observation.take(), /OBSERVATION_MISSING/);
  observation.dispose();
  assert.equal(context.__retromCheckpointUploadObservation, undefined);
  for (const name of Object.keys(original)) {assert.equal(Request.prototype[name], original[name]);}
});

test("observer ignores unrelated origins, previews and methods without reading their bodies", async () => {
  const {Request, observation} = fixture();
  for (const [method, url] of [["POST", "http://another.localhost/runtime/launches/preview-1/save-states"],
    ["POST", "/runtime/launches/another/save-states"], ["GET", "/runtime/launches/preview-1/save-states"]]) {
    const request = new Request();
    const body = {entries() {throw new Error("unrelated body must not be inspected");}};
    request.open(method, url);
    request.send(body);
    assert.equal(request.sentBody, body);
  }
  await assert.rejects(observation.take(), /OBSERVATION_MISSING/);
  observation.dispose();
});

test("duplicate upload observations fail closed but still leave both real requests unchanged", async () => {
  const {Request, observation} = fixture();
  for (let index = 0; index < 2; index += 1) {
    const request = new Request();
    const form = checkpointForm();
    request.open("POST", "/runtime/launches/preview-1/save-states");
    request.send(form);
    assert.equal(request.sentBody, form);
  }
  await assert.rejects(observation.take(), /OBSERVATION_INVALID/);
  observation.dispose();
});
