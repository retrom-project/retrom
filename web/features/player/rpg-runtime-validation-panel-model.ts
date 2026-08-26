import { rpgValidationGates, type RpgPosition } from "./rpg-validation-protocol";
import type { RpgValidationMachineGate, RpgValidationSnapshot } from "./rpg-runtime-validation";

export const validationTerminalSequence = rpgValidationGates.length * 2;

export function canReturnToReview(snapshot: RpgValidationSnapshot) {
  return snapshot.phase === "restore-complete" && snapshot.validationState === "AWAITING_DECISION" &&
    snapshot.lastGateSequence === validationTerminalSequence &&
    snapshot.machineGates.find((gate) => gate.gate === "RESTORE_INPUT")?.status === "PASSED";
}

export function formatRpgPosition(position: RpgPosition | null) {
  if (!position) {return "尚未记录";}
  return `地图 ${position.mapId} · X ${position.playerX} · Y ${position.playerY} · 变量 ${position.fixtureState}`;
}

export function gateEvidenceText(machineGate: RpgValidationMachineGate) {
  if (machineGate.status === "NOT_STARTED") {return "等待服务端证据";}
  if (machineGate.status === "IN_PROGRESS") {return "BEGIN 已接受，等待 PASS / FAIL";}
  if (machineGate.status === "FAILED") {
    return machineGate.failureCode ? `FAIL · ${machineGate.failureCode}` : "FAIL";
  }
  return passedGateEvidenceText(machineGate);
}

function passedGateEvidenceText(machineGate: RpgValidationMachineGate) {
  const evidence = machineGate.evidence;
  if (!evidence) {return "PASS";}
  if ("mapId" in evidence) {
    return formatRpgPosition({
      mapId: evidence.mapId,
      playerX: evidence.playerX,
      playerY: evidence.playerY,
      fixtureState: evidence.fixtureState,
    });
  }
  if ("generation" in evidence) {
    return `${evidence.generation} · ${evidence.engineProfile} · ${evidence.adapterId}`;
  }
  if ("continuousFrames" in evidence) {return `${evidence.continuousFrames} 个连续帧`;}
  if ("payloadKind" in evidence) {
    return `${evidence.payloadKind} · ${evidence.sizeBytes} bytes · SHA-256 ${shortDigest(evidence.sha256)}`;
  }
  if ("observed" in evidence) {
    return machineGate.gate === "AUDIO" ? "实际游戏音频已确认" : "真实输入已观察";
  }
  if (machineGate.gate === "RESTORE_SCREENSHOT") {return "恢复截图已关联";}
  if (machineGate.gate === "RESTORE_STARTED") {return "不同 Restore Launch 已启动";}
  if (machineGate.gate === "ORIGINAL_LAUNCH_ENDED") {return "Original Launch 已结束";}
  if (machineGate.gate === "RUNTIME_READY") {return "运行时已就绪";}
  return "PASS";
}

function shortDigest(value: string) {
  return value.length > 12 ? `${value.slice(0, 12)}…` : value;
}
