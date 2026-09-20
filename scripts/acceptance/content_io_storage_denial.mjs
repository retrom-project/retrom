import assert from "node:assert/strict";
import {targetProtocol} from "./content_io_browser_memory.mjs";

// Pause the already verified production Worker before its first statement and deny
// optional platform APIs through CDP. Provider/module/Worker response bytes stay intact.
export async function denyContentWorkerStorage(context, page) {
  const pageConnection = await context.newCDPSession(page);
  const {targetInfo: pageTarget} = await pageConnection.send("Target.getTargetInfo"); await pageConnection.detach();
  const connection = await context.browser().newBrowserCDPSession(), protocol = targetProtocol(connection);
  const observations = [], errors = [], pending = new Set();
  const targets = new Set(), sessions = [];
  const attach = ({targetInfo}) => {
    if (targetInfo.type !== "worker") return;
    if (targets.has(targetInfo.targetId)) return;
    targets.add(targetInfo.targetId);
    const task = (async () => {
      const {sessionId} = await connection.send("Target.attachToTarget", {targetId: targetInfo.targetId, flatten: false});
      sessions.push(sessionId);
      try {
        const result = await protocol.send(sessionId, "Runtime.evaluate", {returnByValue: true, awaitPromise: true, expression: `(async () => {
          if (self.name !== "retrom-content-io-v1") return {injected: false};
          const denied = async () => { throw new DOMException("Owned acceptance storage denial", "NotAllowedError"); };
          Object.defineProperty(navigator.storage, "getDirectory", {value: denied, configurable: true});
          Object.defineProperty(self, "caches", {value: {open: denied}, configurable: true});
          const attempt = async call => {try {await call(); return "SUCCESS";} catch (error) {return error.name;}};
          return {injected: true, opfs: await attempt(() => navigator.storage.getDirectory()),
            cache: await attempt(() => caches.open("owned-denial-probe"))};
        })()`});
        assert.ok(!result.exceptionDetails, "CONTENT_IO_STORAGE_DENIAL_INJECTION_FAILED");
        if (result.result.value?.injected) {
          assert.deepEqual(result.result.value, {injected: true, opfs: "NotAllowedError", cache: "NotAllowedError"});
          observations.push(result.result.value);
        }
      } finally {
        try {await protocol.send(sessionId, "Runtime.runIfWaitingForDebugger");}
        catch (error) {
          // A short-lived Worker may finish after resuming but before the CDP ACK.
          // Detachment during evaluation/injection is still a failure above.
          if (error.message !== "CONTENT_IO_MEMORY_CDP_TARGET_DETACHED") throw error;
        }
      }
    })().catch(error => errors.push(error.message));
    pending.add(task); void task.finally(() => pending.delete(task));
  };
  connection.on("Target.attachedToTarget", attach);
  await connection.send("Target.autoAttachRelated", {targetId: pageTarget.targetId, waitForDebuggerOnStart: true,
    filter: [{type: "worker", exclude: false}, {exclude: true}]});
  return {async finish() {
    await Promise.all(pending); connection.off("Target.attachedToTarget", attach); protocol.close();
    for (const sessionId of sessions) await connection.send("Target.detachFromTarget", {sessionId}).catch(() => {});
    await connection.detach().catch(() => {});
    assert.deepEqual(errors, []); assert.equal(observations.length, 1, "CONTENT_IO_STORAGE_WORKER_NOT_OBSERVED");
    return observations[0];
  }};
}
