export type NetplayProfile = {
  schemaVersion: 2;
  protocolVersion: "retrom-netplay-v2";
  profileId: string;
  providerId: string;
  targetId: string;
  targetContractSha256: string;
  netplayCompatibilityLine: string;
  coreId: string;
  platformIds: string[];
  gameVariantRevisionId: string;
  sourceManifestDigest: string;
  dependencySnapshotDigest: string;
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

const digest = /^[0-9a-f]{64}$/u;
const identity = /^[a-z0-9]+(?:-[a-z0-9]+)*$/u;
const profileKeys = [
  "canonicalHistoryFrames", "checkpointEveryFrames", "controlCount", "coreId",
  "dependencySnapshotDigest", "gameVariantRevisionId", "maxPlayers", "maxPredictionFrames",
  "maxRollbackFrames", "maxStateBytes", "netplayCompatibilityLine", "platformIds", "profileId",
  "protocolVersion", "providerId", "schemaVersion", "sourceManifestDigest", "targetContractSha256",
  "targetId",
].sort().join(",");

function validProfileIdentity(value: Record<string, unknown>) {
  return typeof value.profileId === "string" && identity.test(value.profileId) &&
    typeof value.providerId === "string" && identity.test(value.providerId) &&
    typeof value.targetId === "string" && identity.test(value.targetId);
}

function validProfileContentIdentity(value: Record<string, unknown>) {
  return typeof value.coreId === "string" && value.coreId.length >= 1 &&
    typeof value.netplayCompatibilityLine === "string" && value.netplayCompatibilityLine.length >= 1 &&
    typeof value.gameVariantRevisionId === "string" && value.gameVariantRevisionId.length >= 1 &&
    [value.targetContractSha256, value.sourceManifestDigest, value.dependencySnapshotDigest]
      .every((entry) => typeof entry === "string" && digest.test(entry));
}

function validProfilePlatforms(value: unknown) {
  return Array.isArray(value) && value.length >= 1 &&
    value.every((entry) => typeof entry === "string" && entry.length >= 1);
}

function validProfileLimits(value: Record<string, unknown>) {
  return value.controlCount === 24 && value.maxRollbackFrames === 120 && value.checkpointEveryFrames === 120 &&
    value.canonicalHistoryFrames === 600 && value.maxStateBytes === 1_048_576 &&
    Number.isSafeInteger(value.maxPlayers) && Number(value.maxPlayers) >= 2 && Number(value.maxPlayers) <= 4 &&
    Number.isSafeInteger(value.maxPredictionFrames) && Number(value.maxPredictionFrames) >= 0 &&
    Number(value.maxPredictionFrames) <= 8;
}

export function parseNetplayProfile(value: Record<string, unknown>): NetplayProfile {
	const valid = Object.keys(value).sort().join(",") === profileKeys && value.schemaVersion === 2 &&
		value.protocolVersion === "retrom-netplay-v2" && validProfileIdentity(value) &&
		validProfileContentIdentity(value) && validProfilePlatforms(value.platformIds) && validProfileLimits(value);
  if (!valid) {throw new Error("PLAYER_NETPLAY_CONFIG_INVALID");}
  return value as NetplayProfile;
}

type CoreMismatchRange = { start: number; end: number };
type AuthorityNormalizationEvidence = {
  epoch: number;
  nextFrame: number;
  attempt: number;
  expectedCoreBytes: number;
  recapturedCoreBytes: number;
  firstCoreMismatch: number;
  lastCoreMismatch: number;
  coreMismatchCount: number;
  coreMismatchRanges: CoreMismatchRange[];
};
type StateLoadEvidence = {
  epoch: number;
  nextFrame: number;
  byteLength: number;
  stateDigest: string;
  coreDigest: string;
  changed: boolean;
  nativeCompletion: boolean;
  byteExact: boolean;
  coreExact: boolean;
  expectedCoreBytes: number;
  recapturedCoreBytes: number;
  firstCoreMismatch: number;
};
type CheckpointBlockDigest = { tag: string; start: number; end: number; digest: string };
type FrameStepEvidence = {
  frame: number;
  phase: "STARTED" | "COMPLETED";
};

export type NetplayDiagnostics = {
  perturbInitialState?: boolean;
  delayForMessage?: (type: string, fields: Record<string, unknown>) => number;
  onConnect?: (reconnect: boolean) => void;
  onStateCapture?: (evidence: { epoch: number; nextFrame: number; byteLength: number; stateDigest: string; coreDigest: string }) => void;
  onAuthorityNormalization?: (evidence: AuthorityNormalizationEvidence) => void;
  onStateLoad?: (evidence: StateLoadEvidence) => void;
  onPause?: (evidence: { epoch: number; reason: string; atFrame: number }) => void;
  onEpoch?: (evidence: { epoch: number; nextFrame: number; resync: boolean }) => void;
  onCanonical?: (evidence: { frame: number; predictionFrames: number }) => void;
  onRollback?: (evidence: { frame: number; through: number; depth: number }) => void;
  onLockstep?: (evidence: { frame: number; inputBufferFrames: number; roundTripMS: number | null }) => void;
  onFrameStep?: (evidence: FrameStepEvidence) => void;
  onRetained?: (evidence: { states: number; predicted: number; canonical: number; stateBytes: number }) => void;
  onCheckpoint?: (evidence: { epoch: number; frame: number; coreDigest: string; stateBlocks?: CheckpointBlockDigest[] }) => void;
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
  if (message === "STATE_LOAD_TIMEOUT" || message === "STATE_INVALID") {return "STATE_INVALID";}
  if (terminalReasons.has(message)) {return message;}
  if (message === "NETPLAY_FRAME_STEP_TIMEOUT" || message === "NETPLAY_FRAME_STEP_INVALID") {return "INTERNAL_ERROR";}
  if (message.startsWith("NETPLAY_") && ["NETPLAY_HISTORY_GAP", "NETPLAY_CANONICAL_INVALID", "NETPLAY_CANONICAL_MUTATED", "NETPLAY_INPUT_INVALID"].includes(message)) {return "PROTOCOL_VIOLATION";}
  return "INTERNAL_ERROR";
}
