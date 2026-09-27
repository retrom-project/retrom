import test from "node:test";
import assert from "node:assert/strict";
import {EventEmitter} from "node:events";
import {executingContentWorker} from "../content_io_worker_identity.mjs";
import {proofDigest} from "../content_io_case_proof.mjs";

function fixture(name = "retrom-content-io-v1", native = false) {
  const url = "blob:http://localhost/owned-worker", source = "// actual verified Worker source\n";
  const connection = new EventEmitter(), operations = [];
  connection.detach = async () => operations.push("detach");
  connection.send = async (method, args) => {
    operations.push(method);
    if (method === "Target.getTargets") return {targetInfos: [...native ? [
      {targetId: "native-pthread", type: "worker", browserContextId: "profile", url: "blob:http://localhost/native"}] : [],
      {targetId: "foreign", type: "worker", browserContextId: "other", url},
      {targetId: "owned", type: "worker", browserContextId: "profile", url}]};
    if (method === "Target.attachToTarget") {assert.equal(args.targetId, "owned"); return {sessionId: "session"};}
    if (method === "Target.sendMessageToTarget") {
      const message = JSON.parse(args.message); let result = {};
      if (message.method === "Runtime.evaluate") result = {result: {value: name}};
      if (message.method === "Debugger.enable") connection.emit("Target.receivedMessageFromTarget", {
        sessionId: args.sessionId, message: JSON.stringify({method: "Debugger.scriptParsed", params: {url, scriptId: "script"}})});
      if (message.method === "Debugger.getScriptSource") {assert.equal(message.params.scriptId, "script"); result = {scriptSource: source};}
      connection.emit("Target.receivedMessageFromTarget", {sessionId: args.sessionId, message: JSON.stringify({id: message.id, result})});
    }
    return {};
  };
  const page = {workers: () => [{url: () => url}, ...native ? [{url: () => "blob:http://localhost/native"}] : []]};
  const context = {newCDPSession: async () => ({send: async () => ({targetInfo: {browserContextId: "profile"}}), detach: async () => {}}),
    browser: () => ({newBrowserCDPSession: async () => connection})};
  return {context, page, source, operations, connection};
}
test("worker identity reads the owned executing Blob script without Network.getResponseBody", async () => {
  const f = fixture(), path = "/runtime/providers/fixture/assets/content-io/worker.mjs";
  assert.deepEqual(await executingContentWorker(f.context, f.page, path), {path, sha256: proofDigest(f.source),
    sizeBytes: Buffer.byteLength(f.source), observation: "EXECUTING_WORKER_SCRIPT"});
  assert.equal(f.operations.at(-1), "detach"); assert.equal(f.connection.listenerCount("Target.receivedMessageFromTarget"), 0);
});
test("a different Worker cannot substitute for the Content Session", async () => {
  const f = fixture("audio-worker");
  await assert.rejects(executingContentWorker(f.context, f.page, "/assets/content-io/worker.mjs"), /EXECUTING_WORKER_MISSING/u);
  assert.equal(f.operations.at(-1), "detach");
});
test("identity observation avoids a native pthread blocked in Atomics.wait", async () => {
  const f = fixture("retrom-content-io-v1", true);
  assert.equal((await executingContentWorker(f.context, f.page, "/assets/content-io/worker.mjs")).sha256, proofDigest(f.source));
  assert.equal(f.operations.filter(value => value === "Target.attachToTarget").length, 1);
});
