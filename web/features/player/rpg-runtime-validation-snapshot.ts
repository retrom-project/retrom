import type { RpgRuntimeConfig as RpgMakerConfig } from "./rpg-runtime";
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
export type RpgValidationResume = NonNullable<RpgMakerConfig["runtimeValidation"]>;
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

export function validValidationResume(config: RpgMakerConfig) {
  const resume = config.runtimeValidation;
  const currentLaunch = resume?.restoreLaunchId === config.launchId || resume?.originalLaunchId === config.launchId;
  const validOrder = resume?.machineGates.length === rpgValidationGates.length &&
    resume.machineGates.every((gate, index) => gate.gate === rpgValidationGates[index]);
  return resume && currentLaunch && validOrder ? resume : null;
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
