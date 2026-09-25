/** Tracks time in the live, visible, unpaused player using a monotonic clock. */
export class PlayProgressClock {
  private elapsed = 0;
  private last = 0;
  private running = false;
  private visible = false;
  private paused = false;

  start(now: number, visible: boolean) {
    this.elapsed = 0;
    this.last = now;
    this.running = true;
    this.visible = visible;
    this.paused = false;
  }

  setVisible(now: number, visible: boolean) {
    this.advance(now);
    this.visible = visible;
  }

  setPaused(now: number, paused: boolean) {
    this.advance(now);
    this.paused = paused;
  }

  snapshot(now: number): number {
    this.advance(now);
    return Math.floor(this.elapsed);
  }

  stop(now: number) {
    this.advance(now);
    this.running = false;
  }

  private advance(now: number) {
    if (Number.isFinite(now) && now >= this.last) {
      if (this.running && this.visible && !this.paused) {this.elapsed += now - this.last;}
      this.last = now;
    }
  }
}
