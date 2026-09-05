import type {PlayerRuntimeV1} from "./runtime/contract";

export async function saveReviewScreenshot(runtime: PlayerRuntimeV1, previewId: string) {
  if (!runtime.getCapabilities().screenshot) {throw new Error("REVIEW_SCREENSHOT_UNSUPPORTED");}
  const screenshot = await runtime.screenshot();
  const response = await fetch(`/runtime/launches/${previewId}/review-screenshot`, {
    method: "POST", credentials: "same-origin",
    headers: {"Content-Type": screenshot.type || "application/octet-stream"}, body: screenshot,
  });
  if (!response.ok) {throw new Error("REVIEW_SCREENSHOT_UPLOAD_FAILED");}
  const value: unknown = await response.json();
  if (!value || typeof value !== "object" || !("importItemId" in value) ||
    typeof value.importItemId !== "string" || !value.importItemId) {
    throw new Error("REVIEW_SCREENSHOT_RECEIPT_INVALID");
  }
  window.opener?.postMessage({
    type: "retrom-review-screenshot", importItemId: value.importItemId, previewId,
  }, window.location.origin);
}
