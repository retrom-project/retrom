import {afterEach, describe, expect, it, vi} from "vitest";
import type {PlayerRuntimeV1} from "./runtime/contract";
import {saveReviewScreenshot} from "./review-preview-screenshot";

afterEach(() => vi.unstubAllGlobals());

describe("ordinary review screenshot", () => {
  const png = new Blob(["png"], {type: "image/png"});
  function runtime(supported = true) {
    return {
      getCapabilities: () => ({screenshot: supported}),
      screenshot: vi.fn(async () => png),
    } as unknown as PlayerRuntimeV1;
  }
  it("uses the launch-scoped authenticated endpoint and notifies only after success", async () => {
    const postMessage = vi.fn();
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({importItemId: "item-1"})));
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("opener", {postMessage});
    await saveReviewScreenshot(runtime(), "preview-1");
    expect(fetchMock).toHaveBeenCalledWith("/runtime/launches/preview-1/review-screenshot", {
      method: "POST", credentials: "same-origin", headers: {"Content-Type": "image/png"}, body: png,
    });
    expect(postMessage).toHaveBeenCalledWith({
      type: "retrom-review-screenshot", importItemId: "item-1", previewId: "preview-1",
    }, window.location.origin);
  });
  it("does not capture or upload when the provider lacks screenshot support", async () => {
    const provider = runtime(false);
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    await expect(saveReviewScreenshot(provider, "preview-1")).rejects.toThrow("REVIEW_SCREENSHOT_UNSUPPORTED");
    expect(provider.screenshot).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
  });
  it.each([401, 410, 500])("does not report success for an upload rejected with %s", async (status) => {
    const postMessage = vi.fn();
    vi.stubGlobal("opener", {postMessage});
    vi.stubGlobal("fetch", vi.fn(async () => new Response("{}", {status})));
    await expect(saveReviewScreenshot(runtime(), "preview-1")).rejects.toThrow("REVIEW_SCREENSHOT_UPLOAD_FAILED");
    expect(postMessage).not.toHaveBeenCalled();
  });
  it.each([null, {}, {importItemId: ""}, {importItemId: 1}])("rejects an invalid receipt before notifying the review", async (value) => {
    const postMessage = vi.fn();
    vi.stubGlobal("opener", {postMessage});
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify(value))));
    await expect(saveReviewScreenshot(runtime(), "preview-1")).rejects.toThrow("REVIEW_SCREENSHOT_RECEIPT_INVALID");
    expect(postMessage).not.toHaveBeenCalled();
  });
});
