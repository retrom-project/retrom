import { describe, expect, it } from "vitest";
import { decodeServerMessage, decodeStateFrame, encodeStateFrame } from "./protocol";

describe("netplay binary protocol", () => {
  it("round trips a state frame with bound session, transfer, epoch and frame", () => {
    const state = new Uint8Array([1, 2, 3]);
    const encoded = encodeStateFrame(
      "01980000-0000-7000-8000-000000000001",
      "01980000-0000-7000-8000-000000000002",
      7,
      1234,
      state,
    );
    expect(decodeStateFrame(encoded)).toEqual({
      sessionId: "01980000-0000-7000-8000-000000000001",
      transferId: "01980000-0000-7000-8000-000000000002",
      epoch: 7,
      nextFrame: 1234,
      state,
    });
  });

  it("rejects invalid identifiers, lengths and state limits", () => {
    expect(() => encodeStateFrame("bad", "01980000-0000-7000-8000-000000000002", 0, 0, new Uint8Array([1]))).toThrow("PROTOCOL_VIOLATION");
    expect(() => encodeStateFrame("01980000-0000-7000-8000-000000000001", "01980000-0000-7000-8000-000000000002", 0, 0, new Uint8Array())).toThrow("PROTOCOL_VIOLATION");
    const frame = encodeStateFrame("01980000-0000-7000-8000-000000000001", "01980000-0000-7000-8000-000000000002", 0, 0, new Uint8Array([1]));
    frame[51] = 2;
    expect(() => decodeStateFrame(frame)).toThrow("PROTOCOL_VIOLATION");
  });
});

describe("netplay server JSON protocol", () => {
  it("accepts a closed canonical message and rejects unknown or malformed fields", () => {
    const message = { v: 1, type: "CANONICAL", sessionId: "01980000-0000-7000-8000-000000000001", epoch: 1, seq: 2, frame: 0, occupiedSeatMask: 3, players: Array.from({ length: 4 }, () => Array(24).fill(0)) };
    expect(decodeServerMessage(JSON.stringify(message))).toMatchObject({ type: "CANONICAL", frame: 0 });
    expect(() => decodeServerMessage(JSON.stringify({ ...message, credential: "secret" }))).toThrow("PROTOCOL_VIOLATION");
    expect(() => decodeServerMessage(JSON.stringify({ ...message, players: [[0], [0], [0], [0]] }))).toThrow("PROTOCOL_VIOLATION");
    expect(() => decodeServerMessage(JSON.stringify({ ...message, seq: 0 }))).toThrow("PROTOCOL_VIOLATION");
  });
});
