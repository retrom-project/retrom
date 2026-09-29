import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import {createContext, runInContext} from "node:vm";
import test from "node:test";

const source = readFileSync(new URL("../../internal/httpapi/web_content/rpc.js", import.meta.url), "utf8");
function fixture(t) {
  t.mock.timers.enable({apis: ["setTimeout"]});
  const sent = [], port = {postMessage: request => sent.push(request), start() {}, close() {}};
  const context = createContext({Uint8Array, setTimeout, clearTimeout});
  runInContext(source, context); context.bindConnection(port);
  return {context, sent, reply: data => port.onmessage({data})};
}
async function read(state, path) {
  const result = state.context.readBlock(path, 0, 1).then(value => ({value}), error => ({error}));
  await Promise.resolve();
  return {result, id: state.sent.at(-1).id};
}
test("queued reads survive more than fifteen seconds while admitted reads complete", async t => {
  const state = fixture(t), queued = await read(state, "queued");
  for (let n = 0; n < 3; n++) {
    const active = await read(state, "active");
    t.mock.timers.tick(10000);
    state.reply({id: active.id, status: 200, bytes: new Uint8Array([7])});
    assert.deepEqual((await active.result).value, new Uint8Array([7]));
  }
  state.reply({id: queued.id, type: "READ_STARTED"});
  state.reply({id: queued.id, status: 200, bytes: new Uint8Array([9])});
  assert.deepEqual((await queued.result).value, new Uint8Array([9]));
});
test("metadata cannot extend a stalled queue and admitted reads keep their own deadline", async t => {
  const state = fixture(t), queued = await read(state, "queued"), active = await read(state, "active");
  state.reply({id: active.id, type: "READ_STARTED"});
  t.mock.timers.tick(10000);
  const metadata = state.context.contentRequest({type: "STAT", path: "metadata"});
  await Promise.resolve();
  state.reply({id: state.sent.at(-1).id, status: 200, sizeBytes: 1}); await metadata;
  t.mock.timers.tick(5000);
  assert.equal((await queued.result).error.message, "CONTENT_IO_TIMEOUT");
  assert.equal((await active.result).error.message, "CONTENT_IO_TIMEOUT");
});
test("other content and duplicate admission notifications cannot renew an active read", async t => {
  const state = fixture(t), active = await read(state, "stalled"), completed = await read(state, "completed");
  state.reply({id: active.id, type: "READ_STARTED"});
  t.mock.timers.tick(10000);
  state.reply({id: active.id, type: "READ_STARTED"});
  state.reply({id: completed.id, status: 200, bytes: new Uint8Array([1])});
  await completed.result;
  t.mock.timers.tick(5000);
  assert.equal((await active.result).error.message, "CONTENT_IO_TIMEOUT");
});
