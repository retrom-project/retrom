import assert from "node:assert/strict";
import {proofDigest} from "./content_io_case_proof.mjs";

// Freeze the owner just before it fails the whole Player. This leaves the actual
// Content Worker alive long enough to verify object-scoped revocation, then lets
// the normal fatal-error/teardown path continue unchanged.
export async function observeDOSFailure(context, page, source, inspect) {
  const marker = 'if (this.state === "FAILED" || this.state === "EXITED") {';
  const lines = source.split("\n"), locations = lines.flatMap((line, index) => line.trim() === marker ? [index] : []);
  assert.equal(locations.length, 1, "DOS_FAILURE_BREAKPOINT_AMBIGUOUS");
  const connection = await context.newCDPSession(page), pending = new Set(), observations = [], errors = [];
  await connection.send("Debugger.enable");
  const {breakpointId} = await connection.send("Debugger.setBreakpointByUrl", {urlRegex: "^blob:", lineNumber: locations[0],
    condition: 'this?.envelope?.runtime?.targetId === "dosbox-pure"'});
  const paused = event => {
    const task = (async () => {
      try {
        const frame = event.callFrames[0]; assert.ok(event.hitBreakpoints.includes(breakpointId));
        const {scriptSource} = await connection.send("Debugger.getScriptSource", {scriptId: frame.location.scriptId});
        assert.equal(proofDigest(scriptSource), proofDigest(source));
        const value = await connection.send("Debugger.evaluateOnCallFrame", {callFrameId: frame.callFrameId,
          expression: "({code, sessionId:this.contentOwner.session.sessionId})", returnByValue: true});
        assert.ok(!value.exceptionDetails);
        observations.push({...value.result.value, ...await inspect(value.result.value)});
        await connection.send("Debugger.removeBreakpoint", {breakpointId});
      } finally {await connection.send("Debugger.resume");}
    })().catch(error => errors.push(error.message));
    pending.add(task); void task.finally(() => pending.delete(task));
  };
  connection.on("Debugger.paused", paused);
  return {snapshot: () => ({observations: [...observations], errors: [...errors]}), async finish() {
    await Promise.all(pending); connection.off("Debugger.paused", paused); await connection.detach();
    assert.deepEqual(errors, []); assert.equal(observations.length, 1); return observations[0];
  }};
}

export async function openDOSAncillary(opened) {
  return opened.page.evaluate(async () => {
    const owner = globalThis.__dosContentAcceptance, player = owner.player;
    const [path, asset] = Object.entries(player.assetIndex).find(([path]) => path.endsWith("/localization/en.json")) ?? [];
    if (!asset) throw Error("DOS_ANCILLARY_ASSET_MISSING");
    const policy = owner.session.inputPolicy("game");
    const source = {identity: {kind: "FILE_SHA256", sha256: asset.sha256}, sizeBytes: asset.sizeBytes,
      url: new URL(path, new URL(player.envelope.runtime.runtimeBaseUrl, location.href)).href,
      purpose: "CORE_ASSET", transport: "RANGE_REQUIRED", etagPolicy: "IMMUTABLE_ASSET", contentLengthPolicy: policy.contentLengthPolicy};
    const reader = await owner.session.open(source, policy); const bytes = new Uint8Array(1); await reader.readInto(0, bytes);
    globalThis.__dosAncillary = reader;
    return {fileId: reader.id, sha256: asset.sha256, sizeBytes: asset.sizeBytes, firstByte: bytes[0]};
  });
}

export async function inspectDOSRevocation(worker, ancillary) {
  const observed = await worker.evaluate(`(async () => {
    const service = globalThis.__dosObservedService;
    const game = [...service.files.values()].find(file => file.object.source.purpose === "GAME");
    const other = service.files.get(${JSON.stringify(ancillary.fileId)});
    if (!game || !other) throw Error("DOS_REVOCATION_FILES_MISSING");
    const read = async (offset, length) => {
      try {await game.reader.readInto(offset, new Uint8Array(length)); return "SUCCESS";} catch (error) {return error.code;}
    };
    const failures = await Promise.all([read(0, 1), read(0, 0), read(262144 + 17, 4096)]);
    const byte = new Uint8Array(1); await other.reader.readInto(0, byte);
    return {failures, unrelatedRevoked: other.object.state.revoked, unrelatedByte: byte[0], sessionClosed: service.closed};
  })()`);
  assert.deepEqual(observed.failures, Array(3).fill("CONTENT_IO_IDENTITY_CHANGED"));
  assert.equal(observed.unrelatedRevoked, false); assert.equal(observed.unrelatedByte, ancillary.firstByte); assert.equal(observed.sessionClosed, false);
  return observed;
}
