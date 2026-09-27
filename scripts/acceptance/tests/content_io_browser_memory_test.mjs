import assert from "node:assert/strict";
import {test} from "node:test";
import {measureBrowserRSS} from "../content_io_browser_memory.mjs";

test("threaded core process measurement does not evaluate blocked workers", async () => {
  const calls = [], connection = {send: async method => {
    calls.push(method); assert.equal(method, "SystemInfo.getProcessInfo");
    return {processInfo: [{id: process.pid, type: "browser"}]};
  }, detach: async () => calls.push("detach")};
  const result = await measureBrowserRSS({newBrowserCDPSession: async () => connection});
  assert.ok(result.processMemoryBytes > 0); assert.equal(result.processes.length, 1);
  assert.equal(result.processMemoryUnavailableReason, null);
  assert.deepEqual(calls, ["SystemInfo.getProcessInfo", "detach"]);
});

test("unavailable process memory is explicit and releases its CDP session", async () => {
  let detached = false;
  const result = await measureBrowserRSS({newBrowserCDPSession: async () => ({
    send: async () => {throw new Error("process exited");}, detach: async () => {detached = true;},
  })});
  assert.equal(result.processMemoryBytes, null); assert.ok(result.processMemoryUnavailableReason);
  assert.deepEqual(result.processes, []); assert.equal(detached, true);
});
