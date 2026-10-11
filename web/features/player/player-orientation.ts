import type {PlayerRuntimeV1} from "./runtime/contract";

export function phoneLandscape(browser: Window): boolean | null {
  const navigator = browser.navigator as Navigator & {userAgentData?: {mobile: boolean}};
  const phone = navigator.userAgentData?.mobile ?? /iPhone|iPod|Android.*Mobile/iu.test(navigator.userAgent);
  if (!phone) { return null; }
  const type = browser.screen.orientation?.type;
  if (type) { return type.startsWith("landscape"); }
  const angle = (browser as Window & {orientation?: number}).orientation;
  if (typeof angle === "number") { return Math.abs(angle) === 90; }
  return browser.matchMedia("(orientation: landscape)").matches;
}

/** Serialize rotations with pause/restore operations and resume only a pause owned by this gate. */
export class OrientationPauseGate {
  private blocked = false;
  private mayResume = false;
  private ownsPause = false;
  private stopped = false;
  private queue: Promise<void> = Promise.resolve();
  constructor(private readonly runtime: PlayerRuntimeV1, private readonly onError: (message: string) => void) {}
  update(blocked: boolean, mayResume: boolean) {
    this.blocked = blocked; this.mayResume = mayResume;
    this.queue = this.queue.then(async () => {
      if (this.stopped) { return; }
      if (this.blocked && this.runtime.getState() === "RUNNING") {
        await this.runtime.pause(); this.ownsPause = true;
      }
      if (!this.stopped && !this.blocked && this.mayResume && this.ownsPause && this.runtime.getState() === "PAUSED") {
        this.ownsPause = false; await this.runtime.resume();
      }
    }).catch(failure => this.onError(failure instanceof Error ? failure.message : "无法暂停游戏，请退出后重试。"));
    return this.queue;
  }
  manualPause() { this.ownsPause = false; }
  stop() { this.stopped = true; }
}
