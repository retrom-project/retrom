import assert from "node:assert/strict";
import {dosOwnerMarker} from "./dosbox_script_markers.mjs";
import {proofDigest} from "./content_io_case_proof.mjs";

// Capture the real adapter and native range facade using a verified-source CDP
// breakpoint. No Provider bytes or method implementations are replaced.
export async function observeDOSContentOwner(context, page, source) {
  const marker = dosOwnerMarker(source);
  const connection = await context.newCDPSession(page), observations = [], errors = [], pending = new Set();
  await connection.send("Debugger.enable");
  const {breakpointId} = await connection.send("Debugger.setBreakpointByUrl", {urlRegex: "^blob:", lineNumber: marker.lineNumber, columnNumber: marker.columnNumber,
    condition: `typeof ${marker.variable} === "object" && this?.envelope?.runtime?.targetId === "dosbox-pure"`});
  const paused = event => {
    const task = (async () => {
      try {
        assert.ok(event.hitBreakpoints.includes(breakpointId));
        const frame = event.callFrames[0];
        const {scriptSource} = await connection.send("Debugger.getScriptSource", {scriptId: frame.location.scriptId});
        assert.equal(proofDigest(scriptSource), proofDigest(source), "DOS_CONTENT_OWNER_SOURCE_CHANGED");
        const value = await connection.send("Debugger.evaluateOnCallFrame", {callFrameId: frame.callFrameId, returnByValue: true,
          expression: `(() => {
            globalThis.__dosContentAcceptance = {player: this, range: ${marker.variable}.range, session: this.contentSession};
            return {targetId: this.envelope.runtime.targetId, sizeBytes: ${marker.variable}.range.sizeBytes, filename: ${marker.variable}.range.filename};
          })()`});
        assert.ok(!value.exceptionDetails); observations.push({...value.result.value, moduleSha256: proofDigest(scriptSource)});
        await connection.send("Debugger.removeBreakpoint", {breakpointId});
      } finally {await connection.send("Debugger.resume");}
    })().catch(error => errors.push(error.message));
    pending.add(task); void task.finally(() => pending.delete(task));
  };
  connection.on("Debugger.paused", paused);
  return {snapshot: () => ({observations: [...observations], errors: [...errors]}), async finish() {
    await Promise.all(pending); connection.off("Debugger.paused", paused); await connection.detach();
    assert.deepEqual(errors, []); assert.equal(observations.length, 1, "DOS_CONTENT_OWNER_NOT_OBSERVED"); return observations[0];
  }};
}

export async function installDOSNativeReader(frame) {
  return frame.evaluate(() => {
    const emulator = globalThis.EJS_emulator, module = emulator.Module, fs = module.FS;
    const path = "/" + emulator.fileName;
    if (typeof module.retromContentFdRead !== "function" || !fs.stat(path).size) throw Error("DOS_NATIVE_READER_MISSING");
    globalThis.__dosNativeRead = async (offset, length) => {
      const stream = fs.open(path, "r"), pointer = module._malloc(Math.max(1, length) + 12);
      try {
        stream.position = offset;
        const view = new DataView(module.HEAPU8.buffer), vector = pointer + Math.max(1, length), count = vector + 8;
        view.setUint32(vector, pointer, true); view.setUint32(vector + 4, length, true); view.setUint32(count, 0, true);
        const code = await module.retromContentFdRead(stream.fd, vector, 1, count);
        const copied = new DataView(module.HEAPU8.buffer).getUint32(count, true);
        return {code, copied, bytes: module.HEAPU8.slice(pointer, pointer + copied)};
      } finally {fs.close(stream); module._free(pointer);}
    };
    return {filename: emulator.fileName, sizeBytes: fs.stat(path).size};
  });
}
