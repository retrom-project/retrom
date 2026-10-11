/** Called before the launch request so the browser still has the user's activation. */
export async function requestPlayerFullscreen(): Promise<Element | null> {
  if (document.fullscreenElement || !document.fullscreenEnabled ||
    typeof document.documentElement.requestFullscreen !== "function") { return null; }
  try {
    await document.documentElement.requestFullscreen();
    return document.documentElement;
  } catch { return null; }
}

export async function cancelLaunchFullscreen(entered: Element | null) {
  if (entered && document.fullscreenElement === entered) {
    // A failed launch returns to its original page; preserve fullscreen that was already active.
    await document.exitFullscreen().catch(() => {});
  }
}
