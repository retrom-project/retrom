import {afterEach, describe, expect, it, vi} from "vitest";
import {notifyReviewCheckpoint} from "./review-preview-receipt";

afterEach(() => vi.unstubAllGlobals());

describe("ordinary review checkpoint receipt", () => {
  const receipt = {
    resourceKind: "REVIEW_PREVIEW_CHECKPOINT", previewId: "preview-1",
    checkpointFormat: "fixture-v1", createdAtMs: 123,
  };
  it("notifies the same-origin opener after each ordinary save", () => {
    const postMessage = vi.fn();
    vi.stubGlobal("opener", {postMessage});
    notifyReviewCheckpoint(JSON.stringify(receipt), "preview-1");
    notifyReviewCheckpoint(JSON.stringify({...receipt, createdAtMs: 124}), "preview-1");
    expect(postMessage).toHaveBeenCalledTimes(2);
    expect(postMessage).toHaveBeenLastCalledWith({
      type: "retrom-review-checkpoint", previewId: "preview-1",
    }, window.location.origin);
  });
  it.each([
    {...receipt, previewId: "another-preview"},
    {...receipt, resourceKind: "SAVE_STATE"},
    {...receipt, createdAtMs: -1},
    {...receipt, checkpointFormat: ""},
    {...receipt, proof: {}},
    null,
  ])("rejects malformed or cross-preview receipts before notification", (value) => {
    const postMessage = vi.fn();
    vi.stubGlobal("opener", {postMessage});
    expect(() => notifyReviewCheckpoint(JSON.stringify(value), "preview-1")).toThrow("REVIEW_CHECKPOINT_RECEIPT_INVALID");
    expect(postMessage).not.toHaveBeenCalled();
  });
});
