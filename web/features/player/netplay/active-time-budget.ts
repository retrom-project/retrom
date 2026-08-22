type VisibilitySource = Pick<Document, "visibilityState" | "addEventListener" | "removeEventListener">;

type ActiveTimeBudgetOptions = {
  now?: () => number;
  visibility?: VisibilitySource;
  setTimer?: (callback: () => void, delayMS: number) => number;
  clearTimer?: (timer: number) => void;
};

/** A one-shot timeout whose budget advances only while the page is visible. */
export class ActiveTimeBudget {
  private remainingMS: number;
  private startedAtMS = 0;
  private timer: number | null = null;
  private settled = false;
  private rejectTimeout: ((error: Error) => void) | null = null;
  private timeoutReason = "ACTIVE_TIME_TIMEOUT";
  private readonly now: () => number;
  private readonly visibility: VisibilitySource;
  private readonly setTimer: (callback: () => void, delayMS: number) => number;
  private readonly clearTimer: (timer: number) => void;
  private readonly onVisibilityChange = () => {
    if (this.settled) {return;}
    if (this.visibility.visibilityState === "hidden") {this.pause();}
    else {this.arm();}
  };

  constructor(budgetMS: number, options: ActiveTimeBudgetOptions = {}) {
    if (!Number.isFinite(budgetMS) || budgetMS <= 0) {throw new Error("ACTIVE_TIME_BUDGET_INVALID");}
    this.remainingMS = budgetMS;
    this.now = options.now ?? (() => performance.now());
    this.visibility = options.visibility ?? document;
    this.setTimer = options.setTimer ?? ((callback, delayMS) => window.setTimeout(callback, delayMS));
    this.clearTimer = options.clearTimer ?? ((timer) => window.clearTimeout(timer));
  }

  race<T>(operation: Promise<T>, timeoutReason: string): Promise<T> {
    if (this.rejectTimeout) {throw new Error("ACTIVE_TIME_BUDGET_ALREADY_STARTED");}
    this.timeoutReason = timeoutReason;
    this.visibility.addEventListener("visibilitychange", this.onVisibilityChange);
    const timeout = new Promise<never>((_, reject) => { this.rejectTimeout = reject; });
    this.arm();
    return Promise.race([operation, timeout]).finally(() => this.finish());
  }

  cancel(reason = "ACTIVE_TIME_BUDGET_CANCELLED") {
    if (this.settled) {return;}
    const reject = this.rejectTimeout;
    this.finish();
    reject?.(new Error(reason));
  }

  private arm() {
    if (this.settled || this.timer !== null || this.visibility.visibilityState === "hidden") {return;}
    this.startedAtMS = this.now();
    this.timer = this.setTimer(() => {
      this.timer = null;
      this.remainingMS = 0;
      const reject = this.rejectTimeout;
      this.finish();
      reject?.(new Error(this.timeoutReason));
    }, this.remainingMS);
  }

  private pause() {
    if (this.timer === null) {return;}
    this.clearTimer(this.timer);
    this.timer = null;
    this.remainingMS = Math.max(0, this.remainingMS - Math.max(0, this.now() - this.startedAtMS));
  }

  private finish() {
    if (this.settled) {return;}
    this.settled = true;
    if (this.timer !== null) {this.clearTimer(this.timer);}
    this.timer = null;
    this.visibility.removeEventListener("visibilitychange", this.onVisibilityChange);
    this.rejectTimeout = null;
  }
}
