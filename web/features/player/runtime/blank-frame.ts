export async function prepareBlankFrame(
  frame: HTMLIFrameElement,
  target: HTMLElement,
  signal: AbortSignal,
) {
  if (signal.aborted) {
    throw new DOMException("Aborted", "AbortError");
  }
  await new Promise<void>((resolve, reject) => {
    const timeout = setTimeout(
      () => finish(new Error("PLAYER_RUNTIME_FRAME_TIMEOUT")),
      10_000,
    );
    function finish(error?: Error) {
      clearTimeout(timeout);
      frame.removeEventListener("load", loaded);
      frame.removeEventListener("error", failed);
      signal.removeEventListener("abort", aborted);
      if (error) {
        frame.remove();
        reject(error);
      } else {
        resolve();
      }
    }
    function loaded() {
      finish();
    }
    function failed() {
      finish(new Error("PLAYER_RUNTIME_FRAME_INVALID"));
    }
    function aborted() {
      finish(new DOMException("Aborted", "AbortError"));
    }
    frame.addEventListener("load", loaded, { once: true });
    frame.addEventListener("error", failed, { once: true });
    signal.addEventListener("abort", aborted, { once: true });
    frame.src = "about:blank";
    target.append(frame);
  });
}
