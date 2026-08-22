import type { EJSNetplayFrameBridge } from "./ejs-netplay-4.2.3-v1";
import { checkpointCoreStateBytes, coreStateBytes, digestHex } from "./ejs-netplay-4.2.3-v1";
import type { NetplayDiagnostics, NetplayProfile } from "./controller-model";
import type { RollbackTimeline } from "./rollback";

export class NetplayCheckpointQueue {
  private readonly pending = new Set<number>();
  private readonly lockstepStates = new Map<number, Uint8Array>();
  private flushing = false;
  private flushRequested = false;

  constructor(
    private readonly profile: NetplayProfile,
    private readonly bridge: EJSNetplayFrameBridge,
    private readonly timeline: RollbackTimeline,
    private readonly diagnostics: NetplayDiagnostics | undefined,
    private readonly send: (frame: number, coreDigest: string) => void,
    private readonly onFailure: (error: unknown) => void,
  ) {}

  reset() {this.pending.clear(); this.lockstepStates.clear();}

  queue(frame: number) {this.pending.add(frame); this.requestFlush();}

  queueLockstep(frame: number) {
    this.lockstepStates.set(frame, this.bridge.captureState());
    this.queue(frame);
  }

  requestFlush() {
    this.flushRequested = true;
    if (!this.flushing) {void this.flush();}
  }

  private async flush() {
    if (this.flushing) {return;}
    this.flushing = true;
    try {
      do {
        this.flushRequested = false;
        for (const frame of [...this.pending].sort((left, right) => left - right)) {
          const after = this.lockstepStates.get(frame) ?? this.timeline.stateAt(frame + 1);
          if (!after) {continue;}
          this.pending.delete(frame); this.lockstepStates.delete(frame);
          const checkpointCore = checkpointCoreStateBytes(coreStateBytes(after), this.profile.profileId);
          const coreDigest = await digestHex(checkpointCore);
          this.diagnostics?.onCheckpoint?.({ frame, coreDigest });
          this.send(frame, coreDigest);
        }
      } while (this.flushRequested);
    } catch (error) {this.onFailure(error);}
    finally {this.flushing = false;}
  }
}
