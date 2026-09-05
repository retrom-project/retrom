import {parseCanonicalJSON} from "./runtime/canonical-json";

export function notifyReviewCheckpoint(body: string, previewId: string) {
  let value: unknown;
  try {
    if (body.length > 4096) {throw new Error("oversized");}
    value = parseCanonicalJSON(body);
  } catch {throw new Error("REVIEW_CHECKPOINT_RECEIPT_INVALID");}
  if (!value || typeof value !== "object" || Array.isArray(value)) {throw invalid();}
  const receipt = value as Record<string, unknown>;
  if (Object.keys(receipt).sort().join(",") !== "checkpointFormat,createdAtMs,previewId,resourceKind" ||
    receipt.resourceKind !== "REVIEW_PREVIEW_CHECKPOINT" || receipt.previewId !== previewId ||
    typeof receipt.checkpointFormat !== "string" || receipt.checkpointFormat.length < 1 ||
    receipt.checkpointFormat.length > 128 || !Number.isSafeInteger(receipt.createdAtMs) ||
    Number(receipt.createdAtMs) < 0) {throw invalid();}
  window.opener?.postMessage({type: "retrom-review-checkpoint", previewId}, window.location.origin);
}

function invalid() {return new Error("REVIEW_CHECKPOINT_RECEIPT_INVALID");}
