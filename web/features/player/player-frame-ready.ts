type FrameReference = { current: HTMLIFrameElement | null };

export async function waitForPlayerFrame(
  reference: FrameReference,
  signal: AbortSignal,
  maximumAttempts = 12,
) {
  for (let attempt = 0; attempt < maximumAttempts && !reference.current; attempt += 1) {
    await nextTimerTurn(signal);
  }
  if (signal.aborted) {throw new DOMException("Aborted", "AbortError");}
  if (!reference.current) {throw new Error("PLAYER_FRAME_UNAVAILABLE");}
  return reference.current;
}

function nextTimerTurn(signal: AbortSignal) {
  if (signal.aborted) {return Promise.reject(new DOMException("Aborted", "AbortError"));}
  return new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(finish, 0);
    signal.addEventListener("abort", abort, {once: true});

    function finish() {
      signal.removeEventListener("abort", abort);
      resolve();
    }
    function abort() {
      window.clearTimeout(timer);
      reject(new DOMException("Aborted", "AbortError"));
    }
  });
}
