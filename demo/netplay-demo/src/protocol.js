export const CONTROL_COUNT = 24;
export const PLAYER_COUNT = 2;

export function assertFrame(frame) {
  if (!Number.isSafeInteger(frame) || frame < 1) {
    throw new TypeError(`Invalid netplay frame: ${frame}`);
  }
  return frame;
}

export function assertSlot(slot) {
  if (!Number.isInteger(slot) || slot < 0 || slot >= PLAYER_COUNT) {
    throw new TypeError(`Invalid player slot: ${slot}`);
  }
  return slot;
}

export function normalizeInputValue(value) {
  if (!Number.isFinite(value)) throw new TypeError(`Invalid controller value: ${value}`);
  return Math.max(-32767, Math.min(32767, Math.round(value)));
}

export function neutralSnapshot() {
  return Object.freeze(Array.from({ length: CONTROL_COUNT }, () => 0));
}

export function normalizeSnapshot(values) {
  if (!Array.isArray(values) && !(values instanceof Int16Array)) {
    throw new TypeError("Controller snapshot must be an array");
  }
  if (values.length !== CONTROL_COUNT) {
    throw new TypeError(`Controller snapshot must contain ${CONTROL_COUNT} values`);
  }
  return Object.freeze(Array.from(values, normalizeInputValue));
}

export function sameSnapshot(left, right) {
  return left.length === right.length && left.every((value, index) => value === right[index]);
}

export function canonicalFrame(frame, players) {
  assertFrame(frame);
  if (!Array.isArray(players) || players.length !== PLAYER_COUNT) {
    throw new TypeError(`Canonical frame must contain ${PLAYER_COUNT} players`);
  }
  return Object.freeze({
    type: "frame",
    frame,
    players: Object.freeze(players.map(normalizeSnapshot))
  });
}

