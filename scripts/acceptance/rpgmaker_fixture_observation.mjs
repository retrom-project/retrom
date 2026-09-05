// Development-only observations. No production Provider probe or review proof API.
// LSD layout/encoding: liblcf 92c4450a1bc1acb58bd02bbb99b57e5036919cdf,
// src/generated/lcf/lsd/chunks.h and src/reader_lcf.cpp:
// https://github.com/EasyRPG/liblcf/tree/92c4450a1bc1acb58bd02bbb99b57e5036919cdf/src
import {createHash} from "node:crypto";

const invalid = () => {throw new Error("RPG_FIXTURE_SAVE_INVALID");};

export function readEasyRpgPosition(checkpoint, engine) {
  try {
    if (!["RPG2000", "RPG2003"].includes(engine) || checkpoint.length < 12 ||
        checkpoint.length > 64 * 1024 * 1024 || checkpoint.subarray(0, 8).toString() !== "RTRPGSV1") {invalid();}
    const manifestSize = checkpoint.readUInt32BE(8);
    if (manifestSize < 1 || manifestSize > 256 * 1024 || 12 + manifestSize >= checkpoint.length) {invalid();}
    const manifest = JSON.parse(checkpoint.subarray(12, 12 + manifestSize).toString("utf8"));
    if (manifest.schemaVersion !== 1 || manifest.engine !== engine || manifest.resumeSlot !== 100 ||
        manifest.entries?.length !== 1) {invalid();}
    const entry = manifest.entries[0];
    const payload = checkpoint.subarray(12 + manifestSize);
    if (entry.store !== "FILESYSTEM" || entry.key !== "Save/Save100.lsd" || entry.offset !== 0 ||
        entry.sizeBytes !== payload.length || entry.mediaType !== "application/octet-stream" ||
        entry.sha256 !== createHash("sha256").update(payload).digest("hex")) {invalid();}
    const cursor = reader(payload);
    if (cursor.take(cursor.integer()).toString("ascii") !== "LcfSaveData") {invalid();}
    const save = chunks(cursor.take(cursor.remaining()));
    const party = chunks(save.get(0x68));
    const system = chunks(save.get(0x65));
    const variables = system.get(0x22);
    const count = fieldInteger(system, 0x21, 0);
    if (count < 1 || !variables || count * 4 !== variables.length) {invalid();}
    const position = {
      mapId: fieldInteger(party, 0x0b, 0), playerX: fieldInteger(party, 0x0c, 0),
      playerY: fieldInteger(party, 0x0d, 0), fixtureState: variables.readInt32LE(0),
    };
    if (!validPosition(position)) {invalid();}
    return position;
  } catch {invalid();}
}

function reader(bytes) {
  if (!Buffer.isBuffer(bytes)) {invalid();}
  let offset = 0;
  function take(length) {
    if (!Number.isSafeInteger(length) || length < 0 || offset + length > bytes.length) {invalid();}
    const value = bytes.subarray(offset, offset + length); offset += length;
    return value;
  }
  return {
    remaining: () => bytes.length - offset, take,
    integer() {
      let value = 0;
      for (let i = 0; i < 5; i += 1) {
        const byte = take(1)[0];
        value = value * 128 + (byte & 0x7f);
        if (value > 0x7fffffff) {invalid();}
        if ((byte & 0x80) === 0) {return value;}
      }
      return invalid();
    },
  };
}

function chunks(bytes) {
  const cursor = reader(bytes);
  const fields = new Map();
  while (cursor.remaining()) {
    const id = cursor.integer();
    if (id === 0) {
      if (cursor.remaining()) {invalid();}
      return fields;
    }
    if (fields.has(id)) {invalid();}
    fields.set(id, cursor.take(cursor.integer()));
  }
  // liblcf allows the outermost Save struct to end at EOF.
  return fields;
}

function fieldInteger(fields, id, defaultValue) {
  if (!fields.has(id)) {return defaultValue;}
  const cursor = reader(fields.get(id));
  const value = cursor.integer();
  if (cursor.remaining()) {invalid();}
  return value;
}

function validPosition(value) {
  return Number.isSafeInteger(value.mapId) && value.mapId > 0 && value.mapId <= 9999 &&
    [value.playerX, value.playerY].every((number) => Number.isSafeInteger(number) && number >= 0 && number < 9999) &&
    [0, 1, 2].includes(value.fixtureState);
}

export function readRgssFixtureLine(line) {
  const match = /(?:^|\s)RETROM_FIXTURE_STATE_V1:(\d+):(\d+):(\d+):(\d+):(\d+)\s*$/.exec(line);
  if (!match) {return null;}
  const [frameCount, mapId, playerX, playerY, fixtureState] = match.slice(1).map(Number);
  const position = {mapId, playerX, playerY, fixtureState};
  if (!Number.isSafeInteger(frameCount) || frameCount < 0 || !validPosition(position)) {return null;}
  return {frameCount, position};
}
