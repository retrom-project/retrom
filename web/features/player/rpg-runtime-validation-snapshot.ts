import type {components} from "@/lib/api/generated/schema";
import {
  rpgValidationGates,
  validateRpgPosition,
  type RpgGate,
  type RpgGateEvidence,
  type RpgPosition,
} from "./rpg-validation-protocol";

export type RpgValidationGateStatus = "NOT_STARTED" | "IN_PROGRESS" | "PASSED" | "FAILED";
export type RpgValidationPhase = "automatic" | "input" | "audio" | "save" | "diverge" | "finish" |
  "original-complete" | "restore-input" | "restore-complete" | "error";
export type RpgValidationResume = {
  validationId: string;
  state: components["schemas"]["RpgRuntimeValidationState"];
  originalLaunchId: string;
  restoreLaunchId: string | null;
  lastGateSequence: number;
  machineGates: components["schemas"]["RpgRuntimeMachineGate"][];
  checkpointEvidence: {
    checkpointFormat: string;
    sizeBytes: number;
    sha256: string;
  } | null;
  restoreScreenshotUploaded: boolean;
};
export type RpgValidationMachineGate = RpgValidationResume["machineGates"][number];

export type RpgValidationSnapshot = {
  phase: RpgValidationPhase;
  title: string;
  message: string;
  actionLabel: string | null;
  busy: boolean;
  error: string | null;
  gates: Readonly<Record<RpgGate, RpgValidationGateStatus>>;
  launchRole: "original" | "restore";
  originalLaunchId: string;
  restoreLaunchId: string | null;
  validationState: RpgValidationResume["state"];
  lastGateSequence: number;
  machineGates: readonly RpgValidationMachineGate[];
  initialPosition: RpgPosition | null;
  savedPosition: RpgPosition | null;
  divergedPosition: RpgPosition | null;
  restoredPosition: RpgPosition | null;
  restoreInputPosition: RpgPosition | null;
  observedPosition: RpgPosition | null;
};

export function initialValidationSnapshot(
  resume: RpgValidationResume,
  currentLaunchId: string,
  restore: boolean,
): RpgValidationSnapshot {
  const gates = Object.fromEntries(resume.machineGates.map((gate) => [gate.gate, gate.status])) as
    Record<RpgGate, RpgValidationGateStatus>;
  return {
    phase: "automatic",
    title: restore ? "正在验证恢复" : "正在执行自动检查",
    message: restore ? "正在恢复检查点并回读地图、坐标与测试变量。" : "正在验证运行时、引擎配置与 300 个连续帧。",
    actionLabel: null,
    busy: true,
    error: null,
    gates,
    launchRole: resume.restoreLaunchId === currentLaunchId ? "restore" : "original",
    originalLaunchId: resume.originalLaunchId,
    restoreLaunchId: resume.restoreLaunchId,
    validationState: resume.state,
    lastGateSequence: resume.lastGateSequence,
    machineGates: resume.machineGates.map(cloneMachineGate),
    initialPosition: gatePosition(resume, "INITIAL_POSITION_RECORDED"),
    savedPosition: gatePosition(resume, "SAVE_POINT_RECORDED"),
    divergedPosition: gatePosition(resume, "POST_SAVE_STATE_DIVERGED"),
    restoredPosition: gatePosition(resume, "RESTORE_POSITION_VERIFIED"),
    restoreInputPosition: gatePosition(resume, "RESTORE_INPUT"),
    observedPosition: gatePosition(resume, restore ? "RESTORE_POSITION_VERIFIED" : "POST_SAVE_STATE_DIVERGED"),
  };
}

export function projectMachineGate(
  machineGates: readonly RpgValidationMachineGate[],
  target: RpgGate,
  status: RpgValidationGateStatus,
  evidence: RpgGateEvidence | null,
  accepted: boolean,
) {
  const observedAtMs = Date.now();
  return machineGates.map((gate) => {
    if (gate.gate !== target) {return gate;}
    const terminal = status === "PASSED" || status === "FAILED";
    return {
      ...gate,
      status,
      begunAtMs: accepted && status === "IN_PROGRESS" ? observedAtMs : gate.begunAtMs,
      completedAtMs: accepted && terminal ? observedAtMs : gate.completedAtMs,
      evidence: accepted && terminal ? evidence : gate.evidence,
    };
  });
}

export function projectValidationState(
  current: RpgValidationResume["state"],
  gate: RpgGate,
  status: RpgValidationGateStatus,
): RpgValidationResume["state"] {
  if (status === "FAILED") {return "FAILED";}
  if (status !== "PASSED") {return current;}
  if (gate === "RUNTIME_READY") {return "RUNNING";}
  if (gate === "CHECKPOINT_CREATED") {return "CHECKPOINTED";}
  if (gate === "RESTORE_POSITION_VERIFIED") {return "RESTORED";}
  if (gate === "RESTORE_INPUT") {return "AWAITING_DECISION";}
  return current;
}

export function projectPositionEvidence(
  gate: RpgGate,
  evidence: RpgGateEvidence | null,
): Partial<RpgValidationSnapshot> {
  const position = evidencePosition(evidence);
  if (!position) {return {};}
  if (gate === "INITIAL_POSITION_RECORDED") {return { initialPosition: position };}
  if (gate === "SAVE_POINT_RECORDED") {return { savedPosition: position };}
  if (gate === "POST_SAVE_STATE_DIVERGED") {return { divergedPosition: position };}
  if (gate === "RESTORE_POSITION_VERIFIED") {return { restoredPosition: position };}
  if (gate === "RESTORE_INPUT") {return { restoreInputPosition: position };}
  return {};
}

export function validValidationResume(value: unknown, currentLaunchId: string) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {return null;}
  const resume = value as RpgValidationResume;
  return validResumeShape(value, resume) && validResumeLaunches(resume, currentLaunchId) &&
    validResumeProgress(resume) && validCheckpointEvidence(resume.checkpointEvidence) &&
    typeof resume.restoreScreenshotUploaded === "boolean" ? resume : null;
}

function validResumeShape(value: object, resume: RpgValidationResume) {
  return Object.keys(value).sort().join(",") ===
    "checkpointEvidence,lastGateSequence,machineGates,originalLaunchId,restoreLaunchId,restoreScreenshotUploaded,state,validationId" &&
    canonicalUuid(resume.validationId) && validValidationState(resume.state);
}

function validResumeLaunches(resume: RpgValidationResume, currentLaunchId: string) {
  const current = resume.restoreLaunchId === currentLaunchId || resume.originalLaunchId === currentLaunchId;
  return current && canonicalUuid(resume.originalLaunchId) &&
    (resume.restoreLaunchId === null || canonicalUuid(resume.restoreLaunchId));
}

function validResumeProgress(resume: RpgValidationResume) {
  const sequence = Number.isSafeInteger(resume.lastGateSequence) && resume.lastGateSequence >= 0 &&
    resume.lastGateSequence <= rpgValidationGates.length * 2;
  const gates = Array.isArray(resume.machineGates) && resume.machineGates.length === rpgValidationGates.length &&
    resume.machineGates.every((gate, index) => validMachineGate(gate, rpgValidationGates[index]!));
  return sequence && gates;
}

function validMachineGate(value: unknown, expectedGate: RpgGate) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {return false;}
  const gate = value as RpgValidationMachineGate;
  const timestamp = (item: unknown) => item === null || Number.isSafeInteger(item) && Number(item) >= 0;
  return Object.keys(value).sort().join(",") === "begunAtMs,completedAtMs,evidence,failureCode,gate,status" &&
    gate.gate === expectedGate && ["NOT_STARTED", "IN_PROGRESS", "PASSED", "FAILED"].includes(gate.status) &&
    timestamp(gate.begunAtMs) && timestamp(gate.completedAtMs) &&
    (gate.evidence === null || typeof gate.evidence === "object" && !Array.isArray(gate.evidence)) &&
    (gate.failureCode === null || typeof gate.failureCode === "string");
}

function validCheckpointEvidence(value: unknown) {
  if (value === null) {return true;}
  if (!value || typeof value !== "object" || Array.isArray(value)) {return false;}
  const evidence = value as NonNullable<RpgValidationResume["checkpointEvidence"]>;
  return Object.keys(value).sort().join(",") === "checkpointFormat,sha256,sizeBytes" &&
    typeof evidence.checkpointFormat === "string" && evidence.checkpointFormat.length > 0 &&
    Number.isSafeInteger(evidence.sizeBytes) && evidence.sizeBytes > 0 &&
    typeof evidence.sha256 === "string" && /^[0-9a-f]{64}$/u.test(evidence.sha256);
}

function validValidationState(value: unknown): value is RpgValidationResume["state"] {
  return typeof value === "string" && ["CREATED", "STARTING", "RUNNING", "CHECKPOINTED", "RESTORED",
    "AWAITING_DECISION", "PASSED", "FAILED", "EXPIRED"].includes(value);
}

function canonicalUuid(value: unknown) {
  return typeof value === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u.test(value);
}

function cloneMachineGate(gate: RpgValidationMachineGate): RpgValidationMachineGate {
  return { ...gate, evidence: gate.evidence ? { ...gate.evidence } : null };
}

function gatePosition(resume: RpgValidationResume, gate: RpgGate) {
  return evidencePosition(resume.machineGates.find((candidate) => candidate.gate === gate)?.evidence ?? null);
}

function evidencePosition(evidence: RpgGateEvidence | null) {
  if (!evidence || !("mapId" in evidence) || !("playerX" in evidence) ||
    !("playerY" in evidence) || !("fixtureState" in evidence)) {return null;}
  const position = {
    mapId: evidence.mapId, playerX: evidence.playerX,
    playerY: evidence.playerY, fixtureState: evidence.fixtureState,
  };
  return validateRpgPosition(position) ? position : null;
}
