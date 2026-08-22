export const NETPLAY_CONTROL_COUNT = 24;
export const NETPLAY_MAX_PREDICTION = 8;
export const NETPLAY_MAX_ROLLBACK = 120;
export const NETPLAY_RING_BUDGET = 128 * 1024 * 1024;

export type Controls = readonly number[];
export type CanonicalInput = readonly [Controls, Controls, Controls, Controls];

function sameControls(left: Controls, right: Controls) {
  return left.length === NETPLAY_CONTROL_COUNT && right.length === NETPLAY_CONTROL_COUNT && left.every((value, index) => value === right[index]);
}

export class RollbackTimeline {
  private readonly states = new Map<number, Uint8Array>();
  private readonly predicted = new Map<number, CanonicalInput>();
  private readonly canonical = new Map<number, CanonicalInput>();
  private stateBytes = 0;
  private highestCanonical = -1;

  constructor(
    private readonly maxRollback = NETPLAY_MAX_ROLLBACK,
    private readonly byteBudget = NETPLAY_RING_BUDGET,
    private readonly maxPrediction = NETPLAY_MAX_PREDICTION,
  ) {
    if (!Number.isSafeInteger(maxPrediction) || maxPrediction < 0 || maxPrediction > NETPLAY_MAX_PREDICTION) {
      throw new Error("NETPLAY_PREDICTION_INVALID");
    }
  }

  canPredict(frame: number) { return frame <= this.highestCanonical + this.maxPrediction; }

  recordOwnedStateBefore(frame: number, state: Uint8Array) {
    if (!Number.isInteger(frame) || frame < 0 || !state.byteLength) {throw new Error("NETPLAY_STATE_INVALID");}
    const previous = this.states.get(frame);
    if (previous) {this.stateBytes -= previous.byteLength;}
    this.states.set(frame, state); this.stateBytes += state.byteLength;
    this.prune();
    if (this.stateBytes > this.byteBudget) {throw new Error("STATE_RING_CAPACITY_EXCEEDED");}
  }

  recordBefore(frame: number, state: Uint8Array) { this.recordOwnedStateBefore(frame, new Uint8Array(state)); }

  recordPrediction(frame: number, input: CanonicalInput) { this.predicted.set(frame, input); this.prune(); }

  receiveCanonical(frame: number, input: CanonicalInput) {
    if (!Number.isInteger(frame) || frame < 0 || input.length !== 4 || input.some((controls) => controls.length !== NETPLAY_CONTROL_COUNT)) {throw new Error("NETPLAY_CANONICAL_INVALID");}
    const existing = this.canonical.get(frame);
    if (existing && !existing.every((controls, index) => sameControls(controls, input[index]!))) {throw new Error("NETPLAY_CANONICAL_MUTATED");}
    this.canonical.set(frame, input);
    while (this.canonical.has(this.highestCanonical + 1)) {this.highestCanonical += 1;}
    this.prune();
    const prediction = this.predicted.get(frame);
    if (!prediction || prediction.every((controls, index) => sameControls(controls, input[index]!))) {return null;}
    if (!this.states.has(frame)) {throw new Error("ROLLBACK_WINDOW_EXCEEDED");}
    return frame;
  }

  earliestDifference(fromFrame: number, throughFrame: number) {
    for (let frame = fromFrame; frame <= throughFrame; frame += 1) {
      const predicted = this.predicted.get(frame); const canonical = this.canonical.get(frame);
      if (predicted && canonical && !predicted.every((controls, index) => sameControls(controls, canonical[index]!))) {return frame;}
    }
    return null;
  }

  rollbackPlan(fromFrame: number, throughFrame: number) {
    const state = this.states.get(fromFrame);
    if (!state || throughFrame - fromFrame + 1 > this.maxRollback) {throw new Error("ROLLBACK_WINDOW_EXCEEDED");}
    const frames: Array<{ frame: number; input: CanonicalInput }> = [];
    for (let frame = fromFrame; frame <= throughFrame; frame += 1) {
      const input = this.canonical.get(frame) ?? this.predicted.get(frame);
      if (!input) {throw new Error("NETPLAY_HISTORY_GAP");}
      frames.push({ frame, input });
    }
    return { state: new Uint8Array(state), frames };
  }

  stateAt(frame: number) { const value = this.states.get(frame); return value ? new Uint8Array(value) : null; }

  canonicalAt(frame: number) {
    const value = this.canonical.get(frame);
    return value ? value.map((controls) => [...controls]) as unknown as CanonicalInput : null;
  }

  retained() {
    return { states: this.states.size, predicted: this.predicted.size, canonical: this.canonical.size, stateBytes: this.stateBytes };
  }

  reset(nextFrame = 0) {
    if (!Number.isSafeInteger(nextFrame) || nextFrame < 0) {throw new Error("NETPLAY_CANONICAL_INVALID");}
    this.states.clear(); this.predicted.clear(); this.canonical.clear(); this.stateBytes = 0;
    this.highestCanonical = nextFrame - 1;
  }

  private prune() {
    const minimum = Math.max(0, this.highestCanonical - this.maxRollback + 1);
    const maximumPrediction = this.highestCanonical + this.maxPrediction;
    for (const [frame, bytes] of this.states) {if (frame < minimum) {
      this.states.delete(frame); this.stateBytes -= bytes.byteLength;
    }}
    while (this.states.size > this.maxRollback + 1) {
      const oldestFrame = Math.min(...this.states.keys());
      const oldest = this.states.get(oldestFrame)!;
      this.states.delete(oldestFrame);
      this.stateBytes -= oldest.byteLength;
    }
    for (const frame of this.canonical.keys()) {if (frame < minimum) {this.canonical.delete(frame);}}
    for (const frame of this.predicted.keys()) {
      if (frame < minimum) {this.predicted.delete(frame);}
      else if (frame > maximumPrediction) {throw new Error("NETPLAY_PREDICTION_INVALID");}
    }
  }
}

export function predictInputs(previous: CanonicalInput | null, localPlayerNo: number, local: Controls): CanonicalInput {
  if (local.length !== NETPLAY_CONTROL_COUNT || localPlayerNo < 1 || localPlayerNo > 4) {throw new Error("NETPLAY_INPUT_INVALID");}
  const zero = () => Array<number>(NETPLAY_CONTROL_COUNT).fill(0);
  const result = ([zero(), zero(), zero(), zero()] as unknown) as [number[], number[], number[], number[]];
  if (previous) {for (let player = 0; player < 4; player += 1) {result[player] = [...previous[player]!];}}
  result[localPlayerNo - 1] = [...local];
  return result;
}
