import assert from "node:assert/strict";

// Change a real persisted block while leaving its checksum receipt intact.
export async function corruptDOSBlock(worker, expectedState) {
  const result = await worker.evaluate(`(async () => {
    const service = globalThis.__dosObservedService;
    const object = [...service.files.values()].find(file => file.object.source.purpose === "GAME").object;
    const backing = service.store.persistent(object);
    if (!backing || backing.resources.data.backend !== "OPFS") throw Error("DOS_OPFS_BACKING_REQUIRED");
    const metadata = backing.resources.metadata;
    const generation = await metadata.generation(backing.key, backing.generation);
    const receipt = await metadata.block(backing.key, backing.generation, 0);
    const bytes = await backing.resources.data.read(receipt, generation.state === "COMPLETE");
    bytes[0] ^= 255; await backing.resources.data.write(receipt, bytes);
    const actual = await backing.resources.data.read(receipt, generation.state === "COMPLETE");
    const digest = async value => Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", value)), x => x.toString(16).padStart(2,"0")).join("");
    return {objectKey: backing.key, generation: backing.generation, state: generation.state,
      localSha256: receipt.localSha256, corruptedSha256: await digest(actual), expectedCorruptedSha256: await digest(bytes),
      checksumUnchanged: (await metadata.block(backing.key, backing.generation, 0)).localSha256 === receipt.localSha256,
      resultLeases: service.jobs.stats.resultLeases};
  })()`);
  assert.equal(result.state, expectedState); assert.equal(result.checksumUnchanged, true);
  assert.equal(result.corruptedSha256, result.expectedCorruptedSha256); assert.notEqual(result.corruptedSha256, result.localSha256);
  return result;
}

export async function publishDOSBacking(opened) {
  const result = await opened.page.evaluate(async () => {
    const owner = globalThis.__dosContentAcceptance, source = owner.range.reader.object.source;
    // Exercise the public materialization/cache contract on the same authorized
    // game identity. The running DOS core keeps its normal RANGE reader.
    const policy = {...owner.session.inputPolicy("game"), mode: "EAGER", bridge: "NONE", result: "BLOB"};
    const reader = await owner.session.open(source, policy);
    const materialized = await owner.session.materialize(reader.id, {kind: "BLOB", maxBytes: source.sizeBytes});
    globalThis.__dosHeldBlob = materialized;
    return {kind: materialized.kind, sizeBytes: materialized.blob.size, receipt: materialized.receipt};
  });
  assert.equal(result.kind, "BLOB"); assert.equal(result.sizeBytes, opened.source.sizeBytes); return result;
}

export async function inspectDOSQuarantine(worker, corrupted) {
  const result = await worker.evaluate(`(async () => {
    const service = globalThis.__dosObservedService;
    const object = [...service.files.values()].find(file => file.object.source.purpose === "GAME").object;
    const backing = service.store.persistent(object), metadata = backing.resources.metadata;
    const generation = await metadata.generation(backing.key, backing.generation);
    const receipt = await metadata.block(backing.key, backing.generation, 0);
    const bytes = await backing.resources.data.read(receipt, true);
    const digest = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)), x => x.toString(16).padStart(2,"0")).join("");
    return {state: generation.state, objectRevoked: object.state.revoked, sha256: digest,
      generation: generation.generation, resultLeases: service.jobs.stats.resultLeases, corruptBlocks: service.store.stats.corruptBlocks};
  })()`);
  assert.equal(result.state, "QUARANTINED"); assert.equal(result.objectRevoked, true);
  assert.equal(result.sha256, corrupted.corruptedSha256); assert.equal(result.generation, corrupted.generation);
  assert.ok(result.resultLeases > 0 && result.corruptBlocks > 0); return result;
}
