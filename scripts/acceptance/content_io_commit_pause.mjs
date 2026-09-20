import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import {targetProtocol} from "./content_io_browser_memory.mjs";

export async function pauseContentCommit(context, page, workerSource, onPaused) {
  const lines = workerSource.split("\n"), marker = "await this.commitBacking(object, receipt, signal);";
  const locations = lines.flatMap((line, index) => line.trim() === marker ? [index] : []);
  assert.equal(locations.length, 1, "CONTENT_IO_COMMIT_BREAKPOINT_AMBIGUOUS");
  const sha = text => createHash("sha256").update(text).digest("hex");
  const pageConnection = await context.newCDPSession(page);
  const {targetInfo: pageTarget} = await pageConnection.send("Target.getTargetInfo"); await pageConnection.detach();
  const connection = await context.browser().newBrowserCDPSession(), protocol = targetProtocol(connection);
  const errors = [], pending = new Set(), observations = [], events = [];
  const targets = new Set(), sessions = [];
  const track = operation => {
    const task = operation().catch(error => errors.push(error.message)); pending.add(task);
    void task.finally(() => pending.delete(task));
  };
  const attached = ({targetInfo}) => {
    if (targetInfo.type !== "worker") return;
    if (targets.has(targetInfo.targetId)) return;
    targets.add(targetInfo.targetId);
    track(async () => {
      // Playwright can replace an automatic attachment during Worker discovery.
      // Keep our commands on an explicitly owned session instead.
      const {sessionId} = await connection.send("Target.attachToTarget", {targetId: targetInfo.targetId, flatten: false});
      sessions.push(sessionId);
      try {
        await protocol.send(sessionId, "Runtime.enable");
        const result = await protocol.send(sessionId, "Runtime.evaluate", {returnByValue: true, awaitPromise: true,
          expression: "({name: self.name})"});
        assert.ok(!result.exceptionDetails);
        if (result.result.value.name !== "retrom-content-io-v1") return;
        events.push("attached");
        await protocol.send(sessionId, "Debugger.enable");
        // Reading self.location before module Worker startup can crash Chrome.
        // Target metadata already supplies the URL without invoking that getter.
        assert.ok(typeof targetInfo.url === "string" && targetInfo.url.length > 0);
        await protocol.send(sessionId, "Debugger.setBreakpointByUrl", {lineNumber: locations[0],
          url: targetInfo.url, condition: 'object.source.purpose === "GAME"'});
        events.push("breakpoint-installed");
      } catch (error) {events.push(`setup-error:${error.message}`); throw error;}
      finally {events.push("resuming-startup"); await protocol.send(sessionId, "Runtime.runIfWaitingForDebugger");}
    });
  };
  const received = ({sessionId, message}) => {
    const event = JSON.parse(message); if (event.method !== "Debugger.paused") return;
    track(async () => {
      events.push(`paused:${event.params.reason}`);
      if (event.params.reason === "Break on start") {await protocol.send(sessionId, "Debugger.resume"); return;}
      try {
        const frame = event.params.callFrames[0];
        const {scriptSource} = await protocol.send(sessionId, "Debugger.getScriptSource", {scriptId: frame.location.scriptId});
        assert.equal(sha(scriptSource), sha(workerSource), "CONTENT_IO_COMMIT_WORKER_CHANGED");
        assert.equal(frame.location.lineNumber, locations[0]);
        const result = await protocol.send(sessionId, "Debugger.evaluateOnCallFrame", {callFrameId: frame.callFrameId,
          expression: "({written, size, kind: request2.kind})", returnByValue: true});
        assert.ok(!result.exceptionDetails); assert.equal(result.result.value.written, result.result.value.size);
        const observed = await onPaused(result.result.value);
        observations.push({workerSha256: sha(scriptSource), ...result.result.value, ...observed});
      } finally {
        await protocol.send(sessionId, "Debugger.resume");
        await protocol.send(sessionId, "Debugger.disable"); events.push("resumed-and-disabled");
      }
    });
  };
  connection.on("Target.attachedToTarget", attached); connection.on("Target.receivedMessageFromTarget", received);
  await connection.send("Target.autoAttachRelated", {targetId: pageTarget.targetId, waitForDebuggerOnStart: true,
    filter: [{type: "worker", exclude: false}, {exclude: true}]});
  return {snapshot: () => ({errors: [...errors], events: [...events], observations: [...observations]}), async finish() {
    await Promise.all(pending); connection.off("Target.attachedToTarget", attached);
    connection.off("Target.receivedMessageFromTarget", received); protocol.close();
    for (const sessionId of sessions) await connection.send("Target.detachFromTarget", {sessionId}).catch(() => {});
    await connection.detach().catch(() => {});
    assert.deepEqual(errors, []); assert.equal(observations.length, 1, "CONTENT_IO_COMMIT_NOT_OBSERVED"); return observations[0];
  }};
}
