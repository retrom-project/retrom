export const toolbarPauseDeadlineMs = 750;
export const screenshotCompletionDeadlineMs = 5_000;

export function captureBeforePause<T>(
  capture: Promise<T>,
  pause: () => void,
  pauseDeadlineMs = toolbarPauseDeadlineMs,
  completionDeadlineMs = screenshotCompletionDeadlineMs,
): Promise<T | null> {
  let paused = false;
  let completionTimer: ReturnType<typeof setTimeout> | undefined;
  const pauseOnce = () => {
    if (paused) return;
    paused = true;
    clearTimeout(pauseTimer);
    pause();
  };
  const pauseTimer = setTimeout(pauseOnce, pauseDeadlineMs);
  const settledCapture = capture.then(
    (value) => {
      pauseOnce();
      return value;
    },
    () => {
      pauseOnce();
      return null;
    },
  );
  const completionDeadline = new Promise<null>((resolve) => {
    completionTimer = setTimeout(() => resolve(null), completionDeadlineMs);
  });
  return Promise.race([settledCapture, completionDeadline]).finally(() => {
    if (completionTimer !== undefined) clearTimeout(completionTimer);
  });
}
