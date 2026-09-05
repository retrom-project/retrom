import assert from "node:assert/strict";
import test from "node:test";
import {decodeCheckpointRequestBody, observeCheckpointUpload} from "./rpgmaker_checkpoint_upload.mjs";

function fixture(response) {
  const listeners = new Map();
  const calls = [];
  let detached = false;
  const session = {
    on: (name, listener) => listeners.set(name, listener),
    async send(method, params) {
      calls.push({method, params});
      if (method === "Network.getRequestPostData") {
        if (response instanceof Error) {throw response;}
        return response;
      }
    },
    async detach() {detached = true;},
  };
  const url = "http://test.localhost/runtime/launches/preview-1/save-states";
  const page = {url: () => "http://test.localhost/play/preview-1",
    context: () => ({newCDPSession: async () => session})};
  const request = {url: () => url, method: () => "POST",
    allHeaders: async () => ({"idempotency-key": "checkpoint-1"})};
  const emit = (overrides = {}) => listeners.get("Network.requestWillBeSent")({
    requestId: "request-1", request: {url, method: "POST", headers: {"Idempotency-Key": "checkpoint-1"}, ...overrides},
  });
  return {page, request, emit, calls, detached: () => detached};
}

test("native binary post data is decoded losslessly, including invalid UTF-8 and zero bytes", () => {
  const bytes = Buffer.from([0, 255, 128, 129, 254, 195, 169, 13, 10]);
  assert.deepEqual(decodeCheckpointRequestBody({postData: bytes.toString("base64"), base64Encoded: true}), bytes);
  assert.equal(decodeCheckpointRequestBody({postData: "中文 metadata"}).toString(), "中文 metadata");
  for (const response of [{}, {postData: ""}, {postData: "%%%", base64Encoded: true},
    {postData: "Zg", base64Encoded: true}, {postData: "text", base64Encoded: "true"}]) {
    assert.throws(() => decodeCheckpointRequestBody(response), /BODY_INVALID/);
  }
});

test("observer reads the exact request body through CDP without installing page scripts", async () => {
  const state = fixture({postData: "native bytes"});
  const observation = await observeCheckpointUpload(state.page, "preview-1");
  state.emit({url: "http://another.localhost/runtime/launches/preview-1/save-states"});
  state.emit({method: "GET"});
  assert.equal(state.calls.length, 1);
  state.emit();
  assert.equal((await observation.take(state.request)).toString(), "native bytes");
  assert.deepEqual(state.calls.map(call => call.method), ["Network.enable", "Network.getRequestPostData"]);
  assert.deepEqual(state.calls[1].params, {requestId: "request-1"});
  await observation.close();
  assert.equal(state.detached(), true);
});

test("missing, duplicated and mismatched request observations fail closed", async () => {
  for (const kind of ["missing", "duplicate", "key", "url", "method"]) {
    const state = fixture({postData: "native bytes"});
    const observation = await observeCheckpointUpload(state.page, "preview-1");
    if (kind !== "missing") {state.emit();}
    if (kind === "duplicate") {state.emit();}
    const request = {...state.request};
    if (kind === "key") {request.allHeaders = async () => ({"idempotency-key": "another"});}
    if (kind === "url") {request.url = () => "http://test.localhost/another";}
    if (kind === "method") {request.method = () => "PUT";}
    await assert.rejects(observation.take(request), /OBSERVATION_MISMATCH/);
    await observation.close();
  }
});

test("a CDP read failure cannot turn into a fabricated checkpoint", async () => {
  const state = fixture(new Error("unavailable"));
  const observation = await observeCheckpointUpload(state.page, "preview-1");
  state.emit();
  await assert.rejects(observation.take(state.request), /BODY_UNAVAILABLE/);
  await observation.close();
});
