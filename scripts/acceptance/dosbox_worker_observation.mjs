import assert from "node:assert/strict";
import {proofDigest} from "./content_io_case_proof.mjs";
import {targetProtocol} from "./content_io_browser_memory.mjs";

// Observe the verified service at construction. Fault scenarios may evict its real
// memory cache, exactly as an empty cache would behave; no read results are mocked.
export async function observeDOSWorker(context, page, source, denyStorage = false) {
  const marker = 'boot.managementPort.onmessageerror = () => this.close(new ContentIOError("INTERNAL"));';
  const locations = source.split("\n").flatMap((line, index) => line.trim() === marker ? [index] : []);
  assert.equal(locations.length, 1, "DOS_WORKER_BREAKPOINT_AMBIGUOUS");
  const pageConnection = await context.newCDPSession(page);
  const {targetInfo: target} = await pageConnection.send("Target.getTargetInfo"); await pageConnection.detach();
  const connection = await context.browser().newBrowserCDPSession(), protocol = targetProtocol(connection);
  const pending = new Set(), errors = [], sessions = [], targets = new Set(), observations = [];
  const closedURLs = new Set();
  const created = worker => worker.once("close", () => closedURLs.add(worker.url()));
  page.on("worker", created);
  let contentSession, contentTargetURL;
  const track = operation => {
    const task = operation().catch(error => errors.push(error.message)); pending.add(task);
    void task.finally(() => pending.delete(task));
  };
  const attached = ({targetInfo}) => {
    if (targetInfo.type !== "worker" || targets.has(targetInfo.targetId)) return;
    targets.add(targetInfo.targetId);
    track(async () => {
      const {sessionId} = await connection.send("Target.attachToTarget", {targetId: targetInfo.targetId, flatten: false});
      sessions.push(sessionId);
      try {
        const name = await protocol.send(sessionId, "Runtime.evaluate", {expression: "self.name", returnByValue: true});
        if (name.result.value !== "retrom-content-io-v1") return;
        contentSession = sessionId; contentTargetURL = targetInfo.url;
        if (denyStorage) {
          const injected = await protocol.send(sessionId, "Runtime.evaluate", {expression: `(() => {
            const denied = async () => {throw new DOMException("Owned acceptance storage denial", "NotAllowedError");};
            Object.defineProperty(navigator.storage, "getDirectory", {value: denied, configurable: true});
            Object.defineProperty(self, "caches", {value: {open: denied}, configurable: true});
            return true;
          })()`, returnByValue: true});
          assert.equal(injected.result.value, true);
        }
        await protocol.send(sessionId, "Debugger.enable");
        await protocol.send(sessionId, "Debugger.setBreakpointByUrl", {url: targetInfo.url, lineNumber: locations[0]});
      } finally {await protocol.send(sessionId, "Runtime.runIfWaitingForDebugger");}
    });
  };
  const received = ({sessionId, message}) => {
    const event = JSON.parse(message); if (event.method !== "Debugger.paused") return;
    track(async () => {
      if (event.params.reason === "Break on start") {await protocol.send(sessionId, "Debugger.resume"); return;}
      try {
        const frame = event.params.callFrames[0];
        const {scriptSource} = await protocol.send(sessionId, "Debugger.getScriptSource", {scriptId: frame.location.scriptId});
        assert.equal(proofDigest(scriptSource), proofDigest(source)); assert.equal(frame.location.lineNumber, locations[0]);
        const value = await protocol.send(sessionId, "Debugger.evaluateOnCallFrame", {callFrameId: frame.callFrameId,
          expression: "globalThis.__dosObservedService = this; ({sessionId:this.address.sessionId})", returnByValue: true});
        assert.ok(!value.exceptionDetails); observations.push({...value.result.value, workerSha256: proofDigest(source), denyStorage});
      } finally {
        await protocol.send(sessionId, "Debugger.resume"); await protocol.send(sessionId, "Debugger.disable");
      }
    });
  };
  connection.on("Target.attachedToTarget", attached); connection.on("Target.receivedMessageFromTarget", received);
  await connection.send("Target.autoAttachRelated", {targetId: target.targetId, waitForDebuggerOnStart: true,
    filter: [{type: "worker", exclude: false}, {exclude: true}]});
  return {snapshot: () => ({observations: [...observations], errors: [...errors]}), async evaluate(expression) {
    assert.ok(contentSession); assert.deepEqual(errors, []);
    const value = await protocol.send(contentSession, "Runtime.evaluate", {expression, returnByValue: true, awaitPromise: true});
    assert.ok(!value.exceptionDetails, JSON.stringify(value.exceptionDetails)); return value.result.value;
  }, async terminate() {
    const actual = page.workers().find(worker => worker.url() === contentTargetURL); assert.ok(actual);
    let timer;
    const closed = new Promise((resolve, reject) => {
      actual.once("close", resolve); timer = setTimeout(() => reject(Error("DOS_WORKER_TERMINATION_NOT_OBSERVED")), 5000);
    });
    try {
      await page.evaluate(() => globalThis.__dosContentAcceptance.player.contentOwner.session.worker.terminate());
      await closed; return {success: true, observation: "ACTUAL_WORKER_CLOSE"};
    } finally {clearTimeout(timer);}
  }, async finish() {
    await Promise.all(pending); connection.off("Target.attachedToTarget", attached); connection.off("Target.receivedMessageFromTarget", received);
    protocol.close(); for (const sessionId of sessions) await connection.send("Target.detachFromTarget", {sessionId}).catch(() => {});
    await connection.detach(); page.off("worker", created);
    assert.deepEqual(errors, []); assert.equal(observations.length, 1);
    return {...observations[0], workerClosed: closedURLs.has(contentTargetURL)};
  }};
}
