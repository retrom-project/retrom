type StatusTone = "synced" | "busy" | "warning";

export class NetplayStatusPublisher {
  private key = "";
  private tone: StatusTone | null = null;
  private waitingTimer: number | null = null;

  constructor(
    private readonly callback: (text: string, tone: StatusTone) => void,
    private readonly canPublishWaiting: () => boolean,
  ) {}

  publish(key: string, text: string, tone: StatusTone) {
    if (key !== "waiting-peer-input") {this.clearWaiting();}
    if (this.key === key && this.tone === tone) {return;}
    this.key = key; this.tone = tone; this.callback(text, tone);
  }

  scheduleWaiting() {
    if (this.waitingTimer !== null || this.key === "waiting-peer-input" || !this.canPublishWaiting()) {return;}
    this.waitingTimer = window.setTimeout(() => {
      this.waitingTimer = null;
      if (this.canPublishWaiting()) {this.publish("waiting-peer-input", "等待其他玩家输入…", "busy");}
    }, 100);
  }

  clear() {this.clearWaiting();}

  private clearWaiting() {
    if (this.waitingTimer !== null) {window.clearTimeout(this.waitingTimer);}
    this.waitingTimer = null;
  }
}
