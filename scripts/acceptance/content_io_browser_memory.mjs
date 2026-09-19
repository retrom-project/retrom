import assert from "node:assert/strict";
import {readFile} from "node:fs/promises";

export function targetProtocol(connection) {
  let sequence = 0;
  const pending = new Map(), contexts = new Map();
  const receive = ({sessionId, message}) => {
    const value = JSON.parse(message);
    if (value.method === "Runtime.executionContextCreated") {
      const current = contexts.get(sessionId) ?? []; current.push(value.params.context); contexts.set(sessionId, current);
    }
    const request = pending.get(value.id);
    if (!request || request.sessionId !== sessionId) return;
    pending.delete(value.id); clearTimeout(request.timer);
    if (value.error) request.reject(new Error(`CONTENT_IO_MEMORY_CDP:${value.error.code}`));
    else request.resolve(value.result);
  };
  const detached = ({sessionId}) => {
    contexts.delete(sessionId);
    for (const [id, request] of pending) if (request.sessionId === sessionId) {
      pending.delete(id); clearTimeout(request.timer); request.reject(new Error("CONTENT_IO_MEMORY_CDP_TARGET_DETACHED"));
    }
  };
  connection.on("Target.receivedMessageFromTarget", receive);
  connection.on("Target.detachedFromTarget", detached);
  return {contexts, send(sessionId, method, params = {}) {
    const id = ++sequence;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {pending.delete(id); reject(new Error("CONTENT_IO_MEMORY_CDP_TIMEOUT"));}, 5000);
      pending.set(id, {sessionId, timer, resolve, reject});
      void connection.send("Target.sendMessageToTarget", {sessionId, message: JSON.stringify({id, method, params})}).catch(error => {
        clearTimeout(timer); pending.delete(id); reject(error);
      });
    });
  }, close() {
    connection.off("Target.receivedMessageFromTarget", receive);
    connection.off("Target.detachedFromTarget", detached);
    for (const request of pending.values()) {clearTimeout(request.timer); request.reject(new Error("CONTENT_IO_MEMORY_CDP_CLOSED"));}
    pending.clear();
  }};
}

async function wasmMemories(connection, protocol, target) {
  const {sessionId} = await connection.send("Target.attachToTarget", {targetId: target.targetId, flatten: false});
  const result = [], objectGroup = "retrom-content-memory-measurement";
  try {
    await protocol.send(sessionId, "Runtime.enable");
    for (const context of protocol.contexts.get(sessionId) ?? []) {
      if (context.auxData?.isDefault === false) continue;
      const prototype = await protocol.send(sessionId, "Runtime.evaluate", {expression: "WebAssembly.Memory.prototype", contextId: context.id, objectGroup});
      assert.ok(prototype.result.objectId && !prototype.exceptionDetails, "CONTENT_IO_MEMORY_PROTOTYPE_MISSING");
      const objects = await protocol.send(sessionId, "Runtime.queryObjects", {prototypeObjectId: prototype.result.objectId, objectGroup});
      const measured = await protocol.send(sessionId, "Runtime.callFunctionOn", {objectId: objects.objects.objectId, returnByValue: true,
        functionDeclaration: "function() { return Array.from(this, memory => ({byteLength: memory.buffer.byteLength, shared: Object.prototype.toString.call(memory.buffer) === '[object SharedArrayBuffer]'})); }"});
      assert.ok(!measured.exceptionDetails && Array.isArray(measured.result.value), "CONTENT_IO_MEMORY_QUERY_FAILED");
      for (const memory of measured.result.value) {
        assert.ok(Number.isSafeInteger(memory.byteLength) && memory.byteLength >= 0 && typeof memory.shared === "boolean", "CONTENT_IO_MEMORY_VALUE_INVALID");
        result.push({targetType: target.type, executionContextId: context.id, ...memory});
      }
    }
    assert.ok((protocol.contexts.get(sessionId) ?? []).length > 0, "CONTENT_IO_MEMORY_CONTEXT_MISSING");
  } finally {
    await protocol.send(sessionId, "Runtime.releaseObjectGroup", {objectGroup}).catch(() => {});
    await connection.send("Target.detachFromTarget", {sessionId});
  }
  return result;
}

// This is aggregate process RSS, which includes shared pages in each process's count.
// Query after the frame/input timing endpoints: Runtime.queryObjects may trigger GC.
export async function measureBrowserMemory(browser) {
  const connection = await browser.newBrowserCDPSession(), protocol = targetProtocol(connection);
  try {
    const {targetInfos} = await connection.send("Target.getTargets");
    const targets = targetInfos.filter(target => ["page", "iframe", "worker"].includes(target.type));
    assert.ok(targets.length > 0, "CONTENT_IO_MEMORY_TARGET_MISSING");
    const memories = [];
    for (const target of targets) memories.push(...await wasmMemories(connection, protocol, target));
    let processes = [], processMemoryBytes = null, processMemoryUnavailableReason = null;
    try {
      const {processInfo} = await connection.send("SystemInfo.getProcessInfo");
      processes = await Promise.all(processInfo.map(async entry => {
        assert.ok(Number.isSafeInteger(entry.id) && entry.id > 0);
        const status = await readFile(`/proc/${entry.id}/status`, "utf8"), rss = /^VmRSS:\s+(\d+) kB$/mu.exec(status);
        assert.ok(rss, "CONTENT_IO_PROCESS_RSS_UNAVAILABLE");
        return {type: entry.type, pid: entry.id, rssBytes: Number(rss[1]) * 1024};
      }));
      processMemoryBytes = processes.reduce((sum, entry) => sum + entry.rssBytes, 0);
      assert.ok(Number.isSafeInteger(processMemoryBytes) && processMemoryBytes > 0);
    } catch {
      processes = []; processMemoryBytes = null; processMemoryUnavailableReason = "Chrome process RSS is unavailable on this host or a process exited during measurement";
    }
    // Shared Wasm memories may be wrappers over the same pthread backing. Keep their actual
    // observations separate; the case's known module topology must identify shared backing.
    return {memories, processMemoryBytes, processMemoryUnavailableReason, processes};
  } finally {protocol.close(); await connection.detach();}
}
