import type {NetplayRuntimePort} from "../runtime/netplay-port-adapter";
import type {NetplayDiagnostics, NetplayProfile} from "./controller-model";
import type {RollbackTimeline} from "./rollback";
import {digestNetplayState} from "./state-digest";

export class NetplayCheckpointQueue {
  private readonly pending = new Map<number, {epoch: number; generation: number}>();
  private readonly lockstepStates = new Map<number, {state: Uint8Array; epoch: number; generation: number}>();
  private flushing = false;
  private flushRequested = false;
  private generation = 0;

  constructor(
    private readonly profile: NetplayProfile,
    private readonly bridge: NetplayRuntimePort,
    private readonly timeline: RollbackTimeline,
    private readonly diagnostics: NetplayDiagnostics | undefined,
    private readonly send: (frame: number, stateDigest: string) => void,
    private readonly onFailure: (error: unknown) => void,
    private readonly digest: (state: Uint8Array) => Promise<string> = digestNetplayState,
  ) {}

  reset() {this.generation += 1; this.pending.clear(); this.lockstepStates.clear();}

  async queueLockstep(frame: number, epoch: number) {
    const generation = this.generation;
    const state = await this.bridge.captureState(frame + 1);
    if (generation !== this.generation) {return;}
    this.lockstepStates.set(frame, {state, epoch, generation});
    this.queue(frame, epoch);
  }

  queue(frame: number, epoch: number) {this.pending.set(frame, {epoch, generation: this.generation}); this.requestFlush();}

  requestFlush() {this.flushRequested = true; if (!this.flushing) {void this.flush();}}

  private async flush() {
    if (this.flushing) {return;}
    this.flushing = true;
    try {
      do {
        this.flushRequested = false;
        for (const frame of [...this.pending.keys()].sort((left, right) => left - right)) {await this.flushFrame(frame);}
      } while (this.flushRequested);
    } catch (error) {this.onFailure(error);}
    finally {this.flushing = false;}
  }

  private async flushFrame(frame: number) {
    const pending = this.pending.get(frame);
    const lockstep = this.lockstepStates.get(frame);
    const state = lockstep?.state ?? this.timeline.stateAt(frame + 1);
    if (!pending || !state) {return;}
    this.pending.delete(frame); this.lockstepStates.delete(frame);
    let stateDigest: string;
    try {stateDigest = await this.digest(state);}
    catch (error) {
      if (pending.generation !== this.generation) {return;}
      throw error;
    }
    if (pending.generation !== this.generation || lockstep &&
      (lockstep.epoch !== pending.epoch || lockstep.generation !== pending.generation)) {return;}
    this.diagnostics?.onCheckpoint?.({epoch: pending.epoch, frame, coreDigest: stateDigest});
    this.send(frame, stateDigest);
  }
}
