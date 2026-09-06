type ReviewPreviewNotification = {
  type: "retrom-review-screenshot" | "retrom-review-checkpoint";
  previewId: string;
  importItemId: string;
};

export function readReviewPreviewNotification(event: MessageEvent<unknown>, itemId: string): ReviewPreviewNotification | null {
  if (event.origin !== window.location.origin || !event.source || !event.data || typeof event.data !== "object") {return null;}
  const message = event.data as { type?: unknown; previewId?: unknown; importItemId?: unknown };
  if ((message.type !== "retrom-review-screenshot" && message.type !== "retrom-review-checkpoint") ||
    message.importItemId !== itemId || typeof message.previewId !== "string" || !message.previewId) {return null;}
  if (!isPreviewWindow(event.source, message.previewId)) {return null;}
  return { type: message.type, previewId: message.previewId, importItemId: itemId };
}

function isPreviewWindow(source: MessageEventSource, previewId: string): boolean {
  // A page reload loses the in-memory popup map, but preserves the child's opener.
  try {
    return "location" in source && !source.closed && source.opener === window &&
      source.location.origin === window.location.origin &&
      source.location.pathname === `/admin/review-previews/${previewId}`;
  } catch {
    // The child may have navigated across origins since it sent the message.
    return false;
  }
}
