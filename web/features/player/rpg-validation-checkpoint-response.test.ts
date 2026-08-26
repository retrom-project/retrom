import { describe, expect, it } from "vitest";
import { parseValidationCheckpointReceipt } from "./rpg-validation-checkpoint-response";

const valid = {
  resourceKind: "RPG_RUNTIME_VALIDATION_CHECKPOINT",
  validationId: "019c9195-8775-73c5-ad38-e1bcdd301b21",
  payloadKind: "NATIVE_SAVE_BUNDLE_V1",
  nativeProfile: "RPGMV_V1",
  resumeSlot: 1,
  sizeBytes: 3,
  sha256: "039058c6f2c0cb492c533b0a4d14ef77cc0f78abccced5287d84a1a2011cfb81",
  createdAtMs: 1,
};

describe("parseValidationCheckpointReceipt", () => {
  it("returns only the server-authoritative checkpoint gate evidence", () => {
    expect(parseValidationCheckpointReceipt(JSON.stringify(valid))).toEqual({
      payloadKind: "NATIVE_SAVE_BUNDLE_V1",
      sizeBytes: 3,
      sha256: valid.sha256,
    });
  });

  it("rejects missing, malformed, or extra response fields", () => {
    expect(() => parseValidationCheckpointReceipt("not-json")).toThrow("RPG_CHECKPOINT_RESPONSE_INVALID");
    expect(() => parseValidationCheckpointReceipt(JSON.stringify({ ...valid, sha256: "bad" })))
      .toThrow("RPG_CHECKPOINT_RESPONSE_INVALID");
    expect(() => parseValidationCheckpointReceipt(JSON.stringify({ ...valid, extra: true })))
      .toThrow("RPG_CHECKPOINT_RESPONSE_INVALID");
  });
});
