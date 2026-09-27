import assert from "node:assert/strict";
import {targetProtocol} from "./content_io_browser_memory.mjs";
import {proofDigest} from "./content_io_case_proof.mjs";

// Network.getResponseBody is not reliable for a module Worker's main script.
// Read the script actually loaded by that owned Worker through the debugger.
export async function executingContentWorker(context, page, assetPath) {
  const pageConnection = await context.newCDPSession(page);
  const {targetInfo: owner} = await pageConnection.send("Target.getTargetInfo"); await pageConnection.detach();
  const connection = await context.browser().newBrowserCDPSession(), protocol = targetProtocol(connection);
  let sessionId, timer, listener;
  try {
    const {targetInfos} = await connection.send("Target.getTargets");
    const urls = new Set(page.workers().map(worker => worker.url()));
    const targets = targetInfos.filter(row => row.type === "worker" && row.browserContextId === owner.browserContextId && urls.has(row.url));
    let target;
    for (const entry of targets) {
      const attached = await connection.send("Target.attachToTarget", {targetId: entry.targetId, flatten: false});
      const name = await protocol.send(attached.sessionId, "Runtime.evaluate", {expression: "self.name", returnByValue: true});
      if (name.result.value === "retrom-content-io-v1") {
        assert.equal(target, undefined, "CONTENT_IO_EXECUTING_WORKER_AMBIGUOUS");
        target = entry; sessionId = attached.sessionId;
      } else await connection.send("Target.detachFromTarget", {sessionId: attached.sessionId});
    }
    assert.ok(target && typeof assetPath === "string" && assetPath.endsWith("/assets/content-io/worker.mjs"), "CONTENT_IO_EXECUTING_WORKER_MISSING");
    const parsed = new Promise((resolve, reject) => {
      timer = setTimeout(() => reject(new Error("CONTENT_IO_EXECUTING_SCRIPT_MISSING")), 5000);
      listener = event => {
        if (event.sessionId !== sessionId) return;
        const message = JSON.parse(event.message);
        if (message.method === "Debugger.scriptParsed" && message.params.url === target.url) resolve(message.params.scriptId);
      };
      connection.on("Target.receivedMessageFromTarget", listener);
    });
    await protocol.send(sessionId, "Debugger.enable");
    const {scriptSource} = await protocol.send(sessionId, "Debugger.getScriptSource", {scriptId: await parsed});
    return {path: assetPath, sha256: proofDigest(scriptSource), sizeBytes: Buffer.byteLength(scriptSource),
      observation: "EXECUTING_WORKER_SCRIPT"};
  } finally {
    clearTimeout(timer); if (listener) connection.off("Target.receivedMessageFromTarget", listener);
    if (sessionId) {
      await protocol.send(sessionId, "Debugger.disable").catch(() => {});
      await connection.send("Target.detachFromTarget", {sessionId}).catch(() => {});
    }
    protocol.close(); await connection.detach().catch(() => {});
  }
}
