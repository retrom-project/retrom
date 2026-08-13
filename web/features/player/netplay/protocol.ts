import type { CanonicalInput } from "./rollback";

export type ServerMessage = {
  v: number; type: string; sessionId: string; epoch: number; seq: number;
  roomVersion?: number; sessionVersion?: number; leaseMs?: number; historyStartFrame?: number; historyEndFrame?: number; playerNo?: number;
  frame?: number; occupiedSeatMask?: number; players?: CanonicalInput;
  transferId?: string; nextFrame?: number; targetPlayerNos?: number[];
  byteLength?: number; stateSha256?: string; coreSha256?: string;
  canonical?: Array<{ frame: number; occupiedSeatMask: number; players: CanonicalInput }>;
  fromFrame?: number; toFrame?: number; atFrame?: number;
  reason?: string; affectedPlayerNo?: number; roomDisposition?: "WAITING" | "ENDED";
};

const baseServerFields = ["v", "type", "sessionId", "epoch", "seq"] as const;
const serverFields: Record<string, { required: string[]; optional?: string[] }> = {
  WELCOME: { required: ["roomVersion", "sessionVersion", "leaseMs", "historyStartFrame", "historyEndFrame", "occupiedSeatMask", "playerNo"] },
  REQUEST_STATE: { required: ["transferId", "nextFrame", "targetPlayerNos", "reason"] },
  STATE_META: { required: ["transferId", "nextFrame", "byteLength", "stateSha256", "coreSha256"] },
  START_EPOCH: { required: ["nextFrame", "occupiedSeatMask"] },
  CANONICAL: { required: ["frame", "occupiedSeatMask", "players"] },
  HISTORY: { required: ["fromFrame", "toFrame", "canonical"] },
  PAUSE: { required: ["reason", "atFrame"], optional: ["affectedPlayerNo"] },
  SESSION_ENDED: { required: ["reason", "roomDisposition"] },
};

function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function safeInteger(value: unknown, minimum = 0) { return Number.isSafeInteger(value) && (value as number) >= minimum; }
function digest(value: unknown) { return typeof value === "string" && /^[0-9a-f]{64}$/.test(value); }
function controls(value: unknown) { return Array.isArray(value) && value.length === 24 && value.every((item) => Number.isInteger(item) && item >= -32768 && item <= 32767); }
function canonical(value: unknown): value is CanonicalInput { return Array.isArray(value) && value.length === 4 && value.every(controls); }
function mask(value: unknown) { return safeInteger(value, 1) && (value as number) <= 15 && ((value as number) & 1) === 1; }

export function decodeServerMessage(encoded: string): ServerMessage {
  let value: unknown;
  try { value = JSON.parse(encoded); } catch { throw new Error("PROTOCOL_VIOLATION"); }
  if (!isRecord(value) || value.v !== 1 || typeof value.type !== "string" || typeof value.sessionId !== "string" || !safeInteger(value.epoch) || !safeInteger(value.seq, 1)) throw new Error("PROTOCOL_VIOLATION");
  const shape = serverFields[value.type];
  if (!shape) throw new Error("PROTOCOL_VIOLATION");
  const allowed = new Set([...baseServerFields, ...shape.required, ...(shape.optional ?? [])]);
  if (Object.keys(value).some((key) => !allowed.has(key)) || shape.required.some((key) => !(key in value))) throw new Error("PROTOCOL_VIOLATION");
  const integers = ["roomVersion", "sessionVersion", "leaseMs", "playerNo", "nextFrame", "byteLength", "frame", "fromFrame", "toFrame", "affectedPlayerNo"];
  if (integers.some((key) => key in value && !safeInteger(value[key]))) throw new Error("PROTOCOL_VIOLATION");
  if (["historyStartFrame", "historyEndFrame", "atFrame"].some((key) => key in value && !safeInteger(value[key], -1))) throw new Error("PROTOCOL_VIOLATION");
  if (["occupiedSeatMask"].some((key) => key in value && !mask(value[key]))) throw new Error("PROTOCOL_VIOLATION");
  if (["transferId", "reason"].some((key) => key in value && typeof value[key] !== "string")) throw new Error("PROTOCOL_VIOLATION");
  if (["stateSha256", "coreSha256"].some((key) => key in value && !digest(value[key]))) throw new Error("PROTOCOL_VIOLATION");
  if ("players" in value && !canonical(value.players)) throw new Error("PROTOCOL_VIOLATION");
  if ("targetPlayerNos" in value && (!Array.isArray(value.targetPlayerNos) || value.targetPlayerNos.some((item) => !safeInteger(item, 2) || item > 4))) throw new Error("PROTOCOL_VIOLATION");
  if ("canonical" in value && (!Array.isArray(value.canonical) || value.canonical.some((item) => !isRecord(item) || !safeInteger(item.frame) || !mask(item.occupiedSeatMask) || !canonical(item.players)))) throw new Error("PROTOCOL_VIOLATION");
  if ("roomDisposition" in value && value.roomDisposition !== "WAITING" && value.roomDisposition !== "ENDED") throw new Error("PROTOCOL_VIOLATION");
  return value as ServerMessage;
}

function uuidBytes(value: string) {
  const normalized = value.replaceAll("-", "");
  if (!/^[0-9a-f]{32}$/.test(normalized)) throw new Error("PROTOCOL_VIOLATION");
  return Uint8Array.from(normalized.match(/../g)!, (pair) => Number.parseInt(pair, 16));
}

function bytesUUID(value: Uint8Array) {
  const hex = [...value].map((byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

export function encodeStateFrame(sessionId: string, transferId: string, epoch: number, nextFrame: number, state: Uint8Array) {
  if (!Number.isSafeInteger(epoch) || epoch < 0 || !Number.isSafeInteger(nextFrame) || nextFrame < 0 || state.byteLength < 1 || state.byteLength > 1_048_576) throw new Error("PROTOCOL_VIOLATION");
  const result = new Uint8Array(52 + state.byteLength);
  result.set(new TextEncoder().encode("RNS1")); result.set(uuidBytes(sessionId), 4); result.set(uuidBytes(transferId), 20);
  const view = new DataView(result.buffer);
  view.setUint32(36, epoch); view.setBigUint64(40, BigInt(nextFrame)); view.setUint32(48, state.byteLength); result.set(state, 52);
  return result;
}

export function decodeStateFrame(value: Uint8Array) {
  if (value.byteLength < 53 || value.byteLength > 52 + 1_048_576 || new TextDecoder().decode(value.subarray(0, 4)) !== "RNS1") throw new Error("PROTOCOL_VIOLATION");
  const view = new DataView(value.buffer, value.byteOffset, value.byteLength);
  const length = view.getUint32(48);
  if (length !== value.byteLength - 52 || length > 1_048_576) throw new Error("PROTOCOL_VIOLATION");
  const next = view.getBigUint64(40);
  if (next > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error("PROTOCOL_VIOLATION");
  return {
    sessionId: bytesUUID(value.subarray(4, 20)), transferId: bytesUUID(value.subarray(20, 36)),
    epoch: view.getUint32(36), nextFrame: Number(next), state: value.slice(52),
  };
}
