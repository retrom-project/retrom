import assert from "node:assert/strict";
import {createHash} from "node:crypto";
import test from "node:test";
import {readEasyRpgPosition, readRgssFixtureLine} from "./rpgmaker_fixture_observation.mjs";

function integer(value) {
  const bytes = [value & 0x7f];
  while ((value = Math.floor(value / 128)) > 0) {bytes.unshift((value & 0x7f) | 0x80);}
  return Buffer.from(bytes);
}
function chunk(id, bytes) {return Buffer.concat([integer(id), integer(bytes.length), bytes]);}
function save({mapId = 1, playerX = 300, playerY = 8, fixtureState = 1} = {}) {
  const variables = Buffer.alloc(8);
  variables.writeInt32LE(fixtureState);
  return Buffer.concat([
    Buffer.from("\x0bLcfSaveData"),
    chunk(0x65, Buffer.concat([chunk(0x21, integer(2)), chunk(0x22, variables), integer(0)])),
    chunk(0x68, Buffer.concat([
      chunk(0x0b, integer(mapId)), chunk(0x0c, integer(playerX)), chunk(0x0d, integer(playerY)), integer(0),
    ])),
    integer(0),
  ]);
}
function checkpoint(payload, engine = "RPG2000", sha256 = null) {
  const manifest = Buffer.from(JSON.stringify({
    schemaVersion: 1, engine, resumeSlot: 100, entries: [{
      store: "FILESYSTEM", key: "Save/Save100.lsd", mediaType: "application/octet-stream",
      offset: 0, sizeBytes: payload.length, sha256: sha256 ?? createHash("sha256").update(payload).digest("hex"),
    }],
  }));
  const size = Buffer.alloc(4); size.writeUInt32BE(manifest.length);
  return Buffer.concat([Buffer.from("RTRPGSV1"), size, manifest, payload]);
}
test("reads real LCF BER positions and little-endian variable 1 from an ordinary checkpoint", () => {
  for (const engine of ["RPG2000", "RPG2003"]) {
    assert.deepEqual(readEasyRpgPosition(checkpoint(save(), engine), engine), {
      mapId: 1, playerX: 300, playerY: 8, fixtureState: 1,
    });
  }
});
test("rejects truncated, wrong-engine and corrupted save observations rather than making up a position", () => {
  const valid = checkpoint(save());
  for (const bytes of [valid.subarray(0, 10), valid.subarray(0, -1), checkpoint(save(), "RPG2003"),
    checkpoint(save(), "RPG2000", "0".repeat(64)), checkpoint(Buffer.from("not a save")),
    checkpoint(Buffer.concat([save(), Buffer.from([0, 1])]))]) {
    assert.throws(() => readEasyRpgPosition(bytes, "RPG2000"), /RPG_FIXTURE_SAVE_INVALID/);
  }
});
test("reads only bounded state emitted by the owned Ruby fixture", () => {
  assert.deepEqual(readRgssFixtureLine("[INFO] RETROM_FIXTURE_STATE_V1:60:1:12:8:2"), {
    frameCount: 60, position: {mapId: 1, playerX: 12, playerY: 8, fixtureState: 2},
  });
  for (const line of ["unrelated log", "RETROM_FIXTURE_STATE_V1:60:1:12:8:3",
    "RETROM_FIXTURE_STATE_V1:60:1:-1:8:1", "RETROM_FIXTURE_STATE_V1:60:1:99999:8:1",
    "RETROM_FIXTURE_STATE_V1:60:1:12:8:1garbage"]) {
    assert.equal(readRgssFixtureLine(line), null);
  }
});
