import {phoneLandscape} from "./player-orientation";

type LockableOrientation = ScreenOrientation & {lock?: (orientation: "landscape") => Promise<void>};
let current: PlayerPresentation | null = null;

/** Capture ownership before the launch gesture changes the browser presentation. */
export function beginPlayerPresentation() {
  current = new PlayerPresentation();
  return current;
}

/** Direct Player navigation also gets a scope; retries never create a new one. */
export function acquirePlayerPresentation() {
  return !current || current.stopped ? beginPlayerPresentation() : current;
}

/** An explicit fullscreen request withdraws an earlier decision to play windowed. */
export function requestPlayerFullscreen(): Promise<Element | null> {
  const scope = current ?? beginPlayerPresentation();
  scope.declined = false;
  return scope.requestFullscreen();
}

/** A deliberate exit is not a lost presentation: automatic recovery stays off until the user asks again. */
export async function exitPlayerFullscreen() {
  if (current) {current.declined = true;}
  if (document.fullscreenElement) {await document.exitFullscreen();}
}

/** Only browsers that can both enter fullscreen and lock the orientation can rotate the phone for the user. */
export function playerLandscapeLockSupported() {
  const orientation = window.screen.orientation as LockableOrientation | undefined;
  return document.fullscreenEnabled && typeof document.documentElement.requestFullscreen === "function" && typeof orientation?.lock === "function";
}

export class PlayerPresentation {
  private readonly previousFullscreen = document.fullscreenElement;
  private readonly orientation = window.screen.orientation as LockableOrientation | undefined;
  private locked = false;
  private pending: Promise<Element | null> | null = null;
  stopped = false;
  /** The user chose to keep playing outside fullscreen. */
  declined = false;

  requestFullscreen(): Promise<Element | null> {
    if (this.stopped) {return Promise.resolve(null);}
    if (this.pending) {return this.pending;}
    const operation = this.enter();
    this.pending = operation;
    void operation.finally(() => {if (this.pending === operation) {this.pending = null;}});
    return operation;
  }

  stop() {this.stopped = true;}

  async restore() {
    // Close the scope before unlock/exit events can cause rotation or focus retries.
    this.stop();
    if (current !== this) {return;}
    if (this.locked) {
      this.locked = false;
      try {this.orientation?.unlock();} catch { /* Unsupported or no longer active. */ }
    }
    await this.restoreFullscreen();
  }

  private async restoreFullscreen() {
    if (current === this && !this.previousFullscreen && document.fullscreenElement === document.documentElement) {
      await document.exitFullscreen().catch(() => {});
    }
  }

  private async enter(): Promise<Element | null> {
    let entered: Element | null = null;
    if (!document.fullscreenElement) {
      if (!document.fullscreenEnabled || typeof document.documentElement.requestFullscreen !== "function") {return null;}
      try {
        await document.documentElement.requestFullscreen();
        entered = document.documentElement;
      } catch {return null;}
    }
    // A request already handed to the browser can settle after leaving the Player.
    if (this.stopped) {await this.restoreFullscreen(); return null;}
    if (phoneLandscape(window) !== null && typeof this.orientation?.lock === "function") {
      let timer: ReturnType<typeof setTimeout> | undefined;
      try {
        this.locked = true;
        await Promise.race([this.orientation.lock("landscape"), new Promise<void>((resolve) => {timer = setTimeout(resolve, 3000);})]);
      } catch { /* The phone keeps its manual rotation fallback. */ }
      finally {clearTimeout(timer);}
    }
    return this.stopped ? null : entered;
  }
}
