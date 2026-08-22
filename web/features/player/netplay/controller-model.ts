export type NetplayProfile = {
  schemaVersion: 1;
  protocolVersion: "retrom-netplay-v1";
  profileId: string;
  emulatorjsVersion: "4.2.3";
  playerAdapterId: "ejs-4.2.3-v2";
  netplayAdapterId: "ejs-netplay-4.2.3-v1";
  coreArtifactId: string;
  coreArtifactSha256: string;
  gameVariantRevisionId: string;
  sourceManifestDigest: string;
  dependencySnapshotDigest: string;
  defaultCoreOptions: Record<string, string>;
  controlCount: 24;
  maxPlayers: number;
  maxPredictionFrames: number;
  maxRollbackFrames: 120;
  checkpointEveryFrames: 120;
  canonicalHistoryFrames: 600;
  maxStateBytes: 1048576;
};

export type NetplayLaunchConfig = {
  roomId: string;
  sessionId: string;
  playerNo: number;
  netplayProfile: NetplayProfile;
  runtimeSocketUrl: string;
};

export type NetplayDiagnostics = {
  perturbInitialState?: boolean;
  delayForMessage?: (type: string, fields: Record<string, unknown>) => number;
  onConnect?: (reconnect: boolean) => void;
  onStateCapture?: (evidence: { byteLength: number; stateDigest: string; coreDigest: string }) => void;
  onStateLoad?: (evidence: { byteLength: number; stateDigest: string; coreDigest: string; changed: boolean; nativeCompletion: boolean; byteExact: boolean; coreExact: boolean; expectedCoreBytes: number; recapturedCoreBytes: number; firstCoreMismatch: number }) => void;
  onEpoch?: (evidence: { epoch: number; nextFrame: number; resync: boolean }) => void;
  onCanonical?: (evidence: { frame: number; predictionFrames: number }) => void;
  onRollback?: (evidence: { frame: number; through: number; depth: number }) => void;
  onLockstep?: (evidence: { frame: number; inputBufferFrames: number; roundTripMS: number | null }) => void;
  onFrameStep?: (evidence: { frame: number; phase: "STARTED" | "COMPLETED" }) => void;
  onRetained?: (evidence: { states: number; predicted: number; canonical: number; stateBytes: number }) => void;
  onCheckpoint?: (evidence: { frame: number; coreDigest: string }) => void;
  onEnded?: (reason: string) => void;
};

export const lockstepFrameDurationMS = 1_000 / 60;
const lockstepInputBufferSafetyMS = 4;
const lockstepMaxInputBufferFrames = 8;

export type LockstepBufferState = { frames: number; roundTripMS: number | null; lowerTargetSamples: number };

export function updateLockstepBuffer(state: LockstepBufferState, sampleMS: number): LockstepBufferState {
  const roundTripMS = state.roundTripMS === null ? sampleMS : state.roundTripMS * 0.75 + sampleMS * 0.25;
  const target = Math.max(1, Math.min(lockstepMaxInputBufferFrames, Math.ceil((roundTripMS + lockstepInputBufferSafetyMS) / lockstepFrameDurationMS)));
  if (target > state.frames) {return { frames: target, roundTripMS, lowerTargetSamples: 0 };}
  if (target === state.frames) {return { frames: state.frames, roundTripMS, lowerTargetSamples: 0 };}
  const lowerTargetSamples = state.lowerTargetSamples + 1;
  if (lowerTargetSamples >= 120) {return { frames: state.frames - 1, roundTripMS, lowerTargetSamples: 0 };}
  return { frames: state.frames, roundTripMS, lowerTargetSamples };
}

type NetplayDiagnosticControls = { dropConnection: (durationMs: number) => void };

declare global {
  interface Window {
    __RETROM_NETPLAY_DIAGNOSTICS_FACTORY__?: (controls: NetplayDiagnosticControls) => NetplayDiagnostics;
  }
}

const terminalReasons = new Set([
  "ROLLBACK_WINDOW_EXCEEDED", "STATE_RING_CAPACITY_EXCEEDED", "STATE_INVALID",
  "NETPLAY_UNSTABLE", "INTERNAL_ERROR", "PROTOCOL_VIOLATION",
]);

export function terminalReason(error: unknown) {
  const message = error instanceof Error ? error.message : "INTERNAL_ERROR";
  if (message === "USER_EXIT") {return message;}
  if (message === "STATE_LOAD_TIMEOUT" || message === "STATE_INVALID" || message.includes("RASTATE")) {return "STATE_INVALID";}
  if (terminalReasons.has(message)) {return message;}
  if (message === "NETPLAY_FRAME_STEP_TIMEOUT") {return "INTERNAL_ERROR";}
  if (message.startsWith("NETPLAY_") && ["NETPLAY_HISTORY_GAP", "NETPLAY_CANONICAL_INVALID", "NETPLAY_CANONICAL_MUTATED", "NETPLAY_INPUT_INVALID"].includes(message)) {return "PROTOCOL_VIOLATION";}
  return "INTERNAL_ERROR";
}
