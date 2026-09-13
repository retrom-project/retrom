import type { DiagnosticEvent } from "./netplay-checkpoints";

// Start from a recovered 1–2 frame pipeline. Startup state transfer can inflate
// the RTT estimate to 7–8 frames, beyond the later 100ms perturbation's effect.
export type LockstepBaseline = DiagnosticEvent & { frame: number; inputBufferFrames: number };

export function lockstepDelayBaseline(events: DiagnosticEvent[]): LockstepBaseline | null {
  const latest = events.filter((event) => event.kind === "lockstep").at(-1);
  if (latest?.inputBufferFrames === undefined || latest.frame === undefined) {return null;}
  if (latest.inputBufferFrames < 1 || latest.inputBufferFrames > 2) {return null;}
  return { ...latest, frame: latest.frame, inputBufferFrames: latest.inputBufferFrames };
}
