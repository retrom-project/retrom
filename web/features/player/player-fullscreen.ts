import {phoneLandscape} from "./player-orientation";

let pending: Promise<Element | null> | null = null;
/** Called before the launch request so the browser still has the user's activation. */
export function requestPlayerFullscreen(): Promise<Element | null> {
  if (pending) {return pending;}
  const operation = enterPlayerFullscreen();
  pending = operation;
  void operation.finally(() => {if (pending === operation) {pending = null;}});
  return operation;
}

async function enterPlayerFullscreen(): Promise<Element | null> {
  let entered: Element | null = null;
  if (!document.fullscreenElement) {
    if (!document.fullscreenEnabled || typeof document.documentElement.requestFullscreen !== "function") {return null;}
    try {
      await document.documentElement.requestFullscreen();
      entered = document.documentElement;
    } catch {return null;}
  }
  const orientation = window.screen.orientation as ScreenOrientation & {lock?: (orientation: "landscape") => Promise<void>};
  if (phoneLandscape(window) !== null && typeof orientation?.lock === "function") {
    let timer: ReturnType<typeof setTimeout> | undefined;
    try {
      await Promise.race([orientation.lock("landscape"), new Promise<void>((resolve) => {timer = setTimeout(resolve, 3000);})]);
    } catch { /* The phone keeps its manual rotation fallback. */ }
    finally {clearTimeout(timer);}
  }
  return entered;
}

export async function cancelLaunchFullscreen(entered: Element | null) {
  if (entered && document.fullscreenElement === entered) {
    // A failed launch returns to its original page; preserve fullscreen that was already active.
    await document.exitFullscreen().catch(() => {});
  }
}
