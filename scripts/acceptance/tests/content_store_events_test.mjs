import test from "node:test";
import assert from "node:assert/strict";
import {runInNewContext} from "node:vm";
import {observeContentStoreEvents, selectedContentBackend} from "../content_store_events.mjs";
test("READY memory remains selected if optional probes fail, while actual backend transitions take precedence", () => {
  const events = [{type: "READY", backend: "MEMORY"}];
  assert.equal(selectedContentBackend(events), "MEMORY");
  events.push({type: "DIAGNOSTIC", backend: null}); assert.equal(selectedContentBackend(events), "MEMORY");
  events.push({type: "BACKEND_READY", backend: "OPFS"}); assert.equal(selectedContentBackend(events), "OPFS");
  events.push({type: "BACKEND_READY", backend: "MEMORY"}); assert.equal(selectedContentBackend(events), "MEMORY");
  assert.equal(selectedContentBackend([]), null);
});
const sessionId = "12345678-1234-4123-8123-123456789012";
async function fixture() {
  class Port extends EventTarget {emit(data) {this.dispatchEvent(new MessageEvent("message", {data}));}}
  class Channel {constructor() {this.port1 = new Port(); this.port2 = new Port();}}
  const host = new EventTarget();
  const realm = {MessageChannel: Channel, performance: {now: () => 17.5}, addEventListener: host.addEventListener.bind(host)};
  await observeContentStoreEvents({addInitScript: async script => runInNewContext(`(${script.toString()})()`, realm)});
  const channel = new realm.MessageChannel(); return {realm, channel, Channel, host};
}
test("[HP-03] diagnostics retain final numeric observations beyond the bounded event history without inventing missing fields", async () => {
  const {realm, channel, Channel} = await fixture();
  assert.ok(channel instanceof Channel);
  for (let index = 0; index < 150; index++) channel.port1.emit({v: 1, type: "DIAGNOSTIC", sessionId, operation: "READ",
    codeNumber: 0, counts: {networkBytes: index, temporaryBytes: index * 2, firstFrameMs: 1.25, signedURL: "secret", unknown: 25}});
  channel.port1.emit({v: 1, type: "DIAGNOSTIC", sessionId, operation: "CLOSE", codeNumber: 0, counts: {temporaryBytes: 0}});
  assert.equal(realm.__retromContentStoreEvents.length, 100);
  const metrics = realm.__retromContentIOMetrics.sessions[sessionId];
  assert.equal(metrics.latest.networkBytes, 149); assert.equal(metrics.latest.temporaryBytes, 0);
  assert.equal(metrics.observedMax.temporaryBytes, 298); assert.equal(metrics.latest.firstFrameMs, 1.25);
  assert.equal(metrics.messages, 151); assert.ok(!Object.hasOwn(metrics.latest, "inputReadyMs"));
  assert.equal(metrics.lastOperation, "CLOSE"); assert.equal(metrics.lastCodeNumber, 0);
  assert.deepEqual(JSON.parse(JSON.stringify(metrics.closeCounts)), {temporaryBytes: 0});
  assert.ok(!JSON.stringify(realm.__retromContentIOMetrics).includes("secret"));
  assert.ok(!JSON.stringify(realm.__retromContentIOMetrics).includes("unknown"));
});

test("[HP-03] an incomplete or failed final CLOSE is not repaired from old observations", async () => {
  const {realm, channel} = await fixture();
  channel.port1.emit({v: 1, type: "DIAGNOSTIC", sessionId, operation: "READ", codeNumber: 0,
    counts: {channels: 0, leases: 0, temporaryBytes: 0}});
  channel.port1.emit({v: 1, type: "DIAGNOSTIC", sessionId, operation: "CLOSE", codeNumber: 4, counts: {leases: 1, path: "secret"}});
  const metrics = realm.__retromContentIOMetrics.sessions[sessionId];
  assert.equal(metrics.lastCodeNumber, 4); assert.equal(metrics.lastOperation, "CLOSE");
  assert.deepEqual(JSON.parse(JSON.stringify(metrics.closeCounts)), {leases: 1});
  assert.ok(!JSON.stringify(metrics).includes("secret"));
});
test("[HP-03] malformed diagnostics cannot become measured zero or cross-session evidence", async () => {
  const {realm, channel} = await fixture();
  channel.port1.emit({v: 1, type: "DIAGNOSTIC", sessionId: "secret", operation: "READ", counts: {networkBytes: 3}});
  channel.port1.emit({v: 1, type: "DIAGNOSTIC", sessionId, operation: "READ", counts: {networkBytes: -1, rangeRequests: 0.5,
    firstFrameMs: Infinity, temporaryBytes: NaN, wholeRequests: 2, path: "private"}});
  const metrics = realm.__retromContentIOMetrics.sessions[sessionId];
  assert.deepEqual(JSON.parse(JSON.stringify(metrics.latest)), {wholeRequests: 2});
  assert.equal(realm.__retromContentIOMetrics.invalidValues, 4);
  assert.equal(Object.keys(realm.__retromContentIOMetrics.sessions).length, 1);
});

test("[HP-03] Host diagnostics retain aggregate L1 counters and exact resource peaks over raw worker snapshots", async () => {
  const {realm, channel, host} = await fixture();
  const emit = counts => host.dispatchEvent(new CustomEvent("retrom:runtime-diagnostic", {detail: {code: "CONTENT_IO_METRICS",
    message: JSON.stringify({sessionId, operation: "READ", codeNumber: 0, counts})}}));
  emit({memoryHitBytes: 37, peakCacheBytesL1: 262144, peakTemporaryBytes: 524288, materializedBytesByBlob: 17});
  channel.port1.emit({v: 1, type: "DIAGNOSTIC", sessionId, operation: "READ", codeNumber: 0, counts: {memoryHitBytes: 0}});
  const metrics = realm.__retromContentIOMetrics.sessions[sessionId];
  assert.equal(metrics.latest.memoryHitBytes, 37); assert.equal(metrics.latest.peakCacheBytesL1, 262144);
  assert.equal(metrics.latest.peakTemporaryBytes, 524288); assert.equal(metrics.latest.materializedBytesByBlob, 17);
  assert.equal(metrics.source, "HOST");
  host.dispatchEvent(new CustomEvent("retrom:runtime-diagnostic", {detail: {code: "CONTENT_IO_METRICS", message: "not json"}}));
  emit({peakCacheBytesL1: 0.5}); assert.equal(realm.__retromContentIOMetrics.invalidValues, 1);
});
