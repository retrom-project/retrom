type QueuedSend = { socket: WebSocket; payload: string | Uint8Array; sendAtMS: number };

export class OrderedSocketSender {
  private sendNotBeforeMS = 0;
  private readonly queue: QueuedSend[] = [];
  private timer: number | null = null;

  constructor(private readonly canSend: (socket: WebSocket) => boolean) {}

  enqueue(socket: WebSocket, payload: string | Uint8Array, delayMS: number) {
    const now = performance.now();
    const sendAt = Math.max(now + delayMS, this.sendNotBeforeMS);
    this.sendNotBeforeMS = sendAt;
    this.queue.push({ socket, payload, sendAtMS: sendAt });
    this.pump();
  }

  reset() {
    if (this.timer !== null) {window.clearTimeout(this.timer);}
    this.timer = null; this.queue.length = 0; this.sendNotBeforeMS = 0;
  }

  private pump() {
    if (this.timer !== null) {return;}
    while (this.queue.length > 0) {
      const next = this.queue[0]!;
      const waitMS = next.sendAtMS - performance.now();
      if (waitMS > 0) {
        this.timer = window.setTimeout(() => {this.timer = null; this.pump();}, waitMS);
        return;
      }
      this.queue.shift();
      if (this.canSend(next.socket)) {next.socket.send(next.payload);}
    }
  }
}
