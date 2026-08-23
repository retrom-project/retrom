import type { EJSNetplayFrameBridge } from "./ejs-netplay-4.2.3-v1";
import { checkpointCoreStateBytes, coreStateBytes, digestHex } from "./ejs-netplay-4.2.3-v1";
import type { NetplayDiagnostics, NetplayProfile } from "./controller-model";
import type { RollbackTimeline } from "./rollback";
import { snesStateBlockDigests } from "./snes-state-diagnostics";

export class NetplayCheckpointQueue {
  private readonly pending = new Map<number, { epoch: number; generation: number }>();
  private readonly lockstepStates = new Map<number, { state: Uint8Array; epoch: number; generation: number }>();
  private flushing = false;
  private flushRequested = false;
  private generation = 0;

  constructor(
    private readonly profile: NetplayProfile,
    private readonly bridge: EJSNetplayFrameBridge,
    private readonly timeline: RollbackTimeline,
    private readonly diagnostics: NetplayDiagnostics | undefined,
    private readonly send: (frame: number, coreDigest: string) => void,
    private readonly onFailure: (error: unknown) => void,
    private readonly digest: (state: Uint8Array) => Promise<string> = digestHex,
  ) {}

  reset() {this.generation += 1; this.pending.clear(); this.lockstepStates.clear();}

  queue(frame: number, epoch: number) {
    this.pending.set(frame, { epoch, generation: this.generation });
    this.requestFlush();
  }

  queueLockstep(frame: number, epoch: number) {
    this.lockstepStates.set(frame, { state: this.bridge.captureState(), epoch, generation: this.generation });
    this.queue(frame, epoch);
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
        for (const frame of [...this.pending.keys()].sort((left, right) => left - right)) {
          await this.flushFrame(frame);
        }
      } while (this.flushRequested);
    } catch (error) {this.onFailure(error);}
    finally {this.flushing = false;}
  }

  private async flushFrame(frame: number) {
    const pending = this.pending.get(frame);
    const lockstep = this.lockstepStates.get(frame);
    const after = lockstep?.state ?? this.timeline.stateAt(frame + 1);
    if (!pending || !after) {return;}
    this.pending.delete(frame); this.lockstepStates.delete(frame);
    const checkpointCore = checkpointCoreStateBytes(coreStateBytes(after), this.profile.profileId);
    const evidence = await this.digestCheckpoint(checkpointCore, pending.generation);
    if (!evidence || lockstep &&
      (lockstep.epoch !== pending.epoch || lockstep.generation !== pending.generation)) {return;}
    this.diagnostics?.onCheckpoint?.({ epoch: pending.epoch, frame, ...evidence });
    this.send(frame, evidence.coreDigest);
  }

  private async digestCheckpoint(checkpointCore: Uint8Array, generation: number) {
    let coreDigest: string;
    let stateBlocks: Awaited<ReturnType<typeof snesStateBlockDigests>> | null;
    try {
      coreDigest = await this.digest(checkpointCore);
      stateBlocks = this.profile.profileId === "snes9x-423-v1" && this.diagnostics?.onCheckpoint
        ? await snesStateBlockDigests(checkpointCore) : null;
    } catch (error) {
      if (generation !== this.generation) {return null;}
      throw error;
    }
    if (generation !== this.generation) {return null;}
    return { coreDigest, ...(stateBlocks ? { stateBlocks } : {}) };
  }
}
