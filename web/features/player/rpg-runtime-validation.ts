import { sha256 } from "@/lib/crypto";
import {
  rpgMakerPositionProbeKind,
  type GameRuntime,
} from "@xxxsen/retrom-runtime";
import {
  captureManualScreenshot,
  captureManualState,
  type EmulatorInstance,
  type ManualStatePayload,
} from "./adapters/ejs-4.2.3-v2";
import type { RpgRuntimeConfig as RpgMakerConfig } from "./rpg-runtime";
import {
  RpgValidationGateClient,
  rpgEngineProfile,
  sameRpgPosition,
  validateRpgPosition,
  type RpgGate,
  type RpgGateEvidence,
  type RpgPosition,
} from "./rpg-validation-protocol";
import type { ValidationCheckpointReceipt } from "./rpg-validation-checkpoint-response";
import {
  initialValidationSnapshot,
  projectMachineGate,
  projectPositionEvidence,
  projectValidationState,
  validValidationResume,
  type RpgValidationGateStatus,
  type RpgValidationPhase,
  type RpgValidationResume,
  type RpgValidationSnapshot,
} from "./rpg-runtime-validation-snapshot";

export type { RpgValidationMachineGate, RpgValidationSnapshot } from "./rpg-runtime-validation-snapshot";

type DriverOptions = {
  config: RpgMakerConfig;
  signal: AbortSignal;
  uploadCheckpoint: (payload: ManualStatePayload) => Promise<ValidationCheckpointReceipt>;
  finishOriginalLaunch: () => Promise<void>;
};

export class RpgRuntimeValidationDriver {
  private readonly config: RpgMakerConfig;
  private readonly signal: AbortSignal;
  private readonly gates: RpgValidationGateClient;
  private readonly uploadCheckpoint: DriverOptions["uploadCheckpoint"];
  private readonly finishOriginalLaunch: DriverOptions["finishOriginalLaunch"];
  private readonly resume: RpgValidationResume;
  private readonly listeners = new Set<() => void>();
  private snapshot: RpgValidationSnapshot;
  private instance: EmulatorInstance | null = null;
  private runtime: GameRuntime | null = null;
  private activeGate: RpgGate | null = null;

  constructor(options: DriverOptions) {
    this.config = options.config;
    this.signal = options.signal;
    this.resume = requireValidationResume(options.config);
    this.gates = new RpgValidationGateClient(
      options.config.launchId, this.resume.lastGateSequence, options.signal,
    );
    this.uploadCheckpoint = options.uploadCheckpoint;
    this.finishOriginalLaunch = options.finishOriginalLaunch;
    this.snapshot = initialValidationSnapshot(this.resume, options.config.launchId, this.isRestore());
  }

  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  getSnapshot = () => this.snapshot;

  async prepare() {
    const gate = this.isRestore() ? "RESTORE_STARTED" : "RUNTIME_READY";
    try {await this.ensureBegin(gate);}
    catch (error) {this.setFatal(error); throw error;}
  }

  async attachRuntime(instance: EmulatorInstance, runtime: GameRuntime) {
    this.instance = instance;
    this.runtime = runtime;
    try {
      if (this.isRestore()) {await this.completeRestore();}
      else {await this.completeAutomaticOriginalGates();}
    } catch (error) {
      await this.failActiveGate(error);
      throw error;
    }
  }

  async reportRuntimeFailure(error: unknown) {
    await this.failActiveGate(error);
  }

  runAction = async () => {
    if (this.snapshot.busy || this.snapshot.phase === "error") {return;}
    this.patch({ busy: true, error: null });
    try {
      await this.runCurrentAction();
    } catch (error) {
      if (this.activeGate) {await this.failActiveGate(error);}
      else {this.patch({ busy: false, error: errorMessage(error) });}
    }
  };

  private async runCurrentAction() {
    switch (this.snapshot.phase) {
    case "input": await this.confirmInput(); break;
    case "audio": await this.confirmAudio(); break;
    case "save": await this.createCheckpoint(); break;
    case "diverge": await this.recordDivergence(); break;
    case "finish": await this.finishSession(); break;
    case "restore-input": await this.confirmRestoreInput(); break;
    default: this.patch({ busy: false });
    }
  }

  private async completeAutomaticOriginalGates() {
    const runtime = this.requireRuntime();
    await this.completeGate("RUNTIME_READY", {});
    await this.completeGate("ENGINE_PROFILE", {
      generation: this.config.generation,
      adapterId: this.config.adapter.adapterId,
      engineProfile: rpgEngineProfile(this.config.generation),
    });
    if (!this.gatePassed("FRAMES_300")) {
      await this.ensureBegin("FRAMES_300");
      await this.pass("FRAMES_300", { continuousFrames: await waitForContinuousFrames(runtime, this.signal) });
    }
    await this.resumeOriginalActions();
  }

  private async confirmInput() {
    const position = readPosition(this.requireRuntime());
    await this.passPair("INPUT", { observed: true });
    this.patch({ observedPosition: position });
    this.setPhase("audio", "保持游戏音量开启，确认已经听到当前游戏实际播放的声音。", "已听到游戏音频");
  }

  private async confirmAudio() {
    await this.passPair("AUDIO", { observed: true });
    await this.recordInitialPosition();
    this.setPhase("save", "移动到希望恢复的位置 B，并停下操作；下一步会记录位置并上传服务端检查点。", "记录 B 并创建检查点");
  }

  private async resumeOriginalActions() {
    if (!this.gatePassed("INPUT")) {
      this.setPhase("input", "请在游戏中操作角色或测试变量，然后确认输入已经生效。", "输入已经生效");
      return;
    }
    if (!this.gatePassed("AUDIO")) {
      this.setPhase("audio", "保持游戏音量开启，确认已经听到当前游戏实际播放的声音。", "已听到游戏音频");
      return;
    }
    await this.recordInitialPosition();
    if (!this.gatePassed("CHECKPOINT_CREATED")) {
      this.setPhase("save", "移动到希望恢复的位置 B，并停下操作；下一步会记录位置并上传服务端检查点。", "记录 B 并创建检查点");
      return;
    }
    if (!this.gatePassed("ORIGINAL_LAUNCH_ENDED")) {
      this.setPhase("diverge", "检查点已锁定为 B。请继续移动或改变测试变量到不同位置 C，再结束原运行。", "记录 C 并结束原运行");
      return;
    }
    await this.finishSession();
  }

  private async recordInitialPosition() {
    if (this.gatePassed("INITIAL_POSITION_RECORDED")) {return;}
    const initial = await waitForRpgPosition(this.requireRuntime(), this.signal);
    await this.passPair("INITIAL_POSITION_RECORDED", initial);
    this.patch({ initialPosition: initial, observedPosition: initial });
  }

  private async createCheckpoint() {
    const instance = this.requireInstance();
    const runtime = this.requireRuntime();
    const availability = instance.gameManager?.getCheckpointAvailability?.();
    if (availability?.available !== true) {
      this.patch({ busy: false, error: checkpointUnavailableMessage(availability?.reason) });
      return;
    }
    if (await this.finishUploadedCheckpoint()) {return;}
    const before = readPosition(runtime);
    const screenshot = await captureManualScreenshot(instance);
    const payload = await captureManualState(instance, screenshot);
    const after = readPosition(runtime);
    if (!sameRpgPosition(before, after)) {
      this.patch({ busy: false, error: "创建检查点期间位置发生变化，请在 B 点停下后重试。", observedPosition: after });
      return;
    }
    if (!this.snapshot.initialPosition || sameRpgPosition(after, this.snapshot.initialPosition)) {
      this.patch({ busy: false, error: "B 必须不同于初始位置 A，请先移动或改变测试变量。", observedPosition: after });
      return;
    }
    if (this.gatePassed("SAVE_POINT_RECORDED")) {
      if (!this.snapshot.savedPosition || !sameRpgPosition(after, this.snapshot.savedPosition)) {
        this.patch({ busy: false, error: "刷新后仍须停在服务端已记录的 B 点才能继续创建检查点。", observedPosition: after });
        return;
      }
    } else {
      await this.passPair("SAVE_POINT_RECORDED", after);
    }
    await this.ensureBegin("CHECKPOINT_CREATED");
    const receipt = await this.uploadCheckpoint(payload);
    await this.pass("CHECKPOINT_CREATED", await checkpointEvidence(payload, receipt));
    this.patch({ savedPosition: after, observedPosition: after });
    this.setPhase("diverge", "检查点已锁定为 B。请继续移动或改变测试变量到不同位置 C，再结束原运行。", "记录 C 并结束原运行");
  }

  private async finishUploadedCheckpoint() {
    const evidence = this.resume.checkpointEvidence;
    if (this.gateStatus("CHECKPOINT_CREATED") !== "IN_PROGRESS" || !evidence) {return false;}
    await this.pass("CHECKPOINT_CREATED", evidence);
    this.setPhase("diverge", "检查点已锁定为 B。请继续移动或改变测试变量到不同位置 C，再结束原运行。", "记录 C 并结束原运行");
    return true;
  }

  private async recordDivergence() {
    const position = readPosition(this.requireRuntime());
    if (!this.snapshot.savedPosition || sameRpgPosition(position, this.snapshot.savedPosition)) {
      this.patch({ busy: false, error: "C 必须与 B 至少有一个字段不同，请继续操作游戏。", observedPosition: position });
      return;
    }
    if (!this.gatePassed("POST_SAVE_STATE_DIVERGED")) {
      await this.passPair("POST_SAVE_STATE_DIVERGED", position);
    }
    if (!this.gatePassed("ORIGINAL_LAUNCH_ENDED")) {
      await this.passPair("ORIGINAL_LAUNCH_ENDED", {});
    }
    this.patch({ observedPosition: position });
    await this.finishSession();
  }

  private async finishSession() {
    try {
      await this.finishOriginalLaunch();
      this.setPhase("original-complete", "原运行已结束。请返回审核页并点击“验证恢复”，创建不同的恢复 Launch。", null);
    } catch {
      this.setPhase("finish", "机器 gate 已完成，但结束 PlaySession 失败；恢复 Launch 尚不能创建。", "重试结束原运行");
      throw new Error("PLAY_SESSION_EVENT_FAILED");
    }
  }

  private async completeRestore() {
    const instance = this.requireInstance();
    const runtime = this.requireRuntime();
    await this.completeGate("RESTORE_STARTED", {});
    if (!this.gatePassed("RESTORE_POSITION_VERIFIED")) {
      const restored = readPosition(runtime);
      await this.passPair("RESTORE_POSITION_VERIFIED", restored);
      this.patch({ observedPosition: restored });
    }
    if (!this.gatePassed("RESTORE_SCREENSHOT")) {
      await this.ensureBegin("RESTORE_SCREENSHOT");
      if (!this.resume.restoreScreenshotUploaded) {
        await uploadRestoreScreenshot(this.config.launchId, instance, this.signal);
      }
      await this.pass("RESTORE_SCREENSHOT", {});
    }
    if (this.gatePassed("RESTORE_INPUT")) {
      this.setPhase("restore-complete", "恢复位置、截图和恢复后输入均已由服务端验证。请返回审核页决定 PASS/FAIL。", null);
      return;
    }
    this.setPhase("restore-input", "恢复证据已锁定。请继续移动或改变测试变量，证明恢复后的游戏仍可操作。", "恢复后输入已经生效");
  }

  private async confirmRestoreInput() {
    const position = readPosition(this.requireRuntime());
    const restored = this.snapshot.savedPosition ?? this.snapshot.observedPosition;
    if (!restored || sameRpgPosition(position, restored)) {
      this.patch({ busy: false, error: "尚未检测到恢复后的位置或测试变量变化，请先操作游戏。", observedPosition: position });
      return;
    }
    await this.passPair("RESTORE_INPUT", position);
    this.patch({ observedPosition: position });
    this.setPhase("restore-complete", "恢复位置、截图和恢复后输入均已由服务端验证。请返回审核页决定 PASS/FAIL。", null);
  }

  private async completeGate(gate: RpgGate, evidence: RpgGateEvidence) {
    if (this.gatePassed(gate)) {return;}
    await this.ensureBegin(gate);
    await this.pass(gate, evidence);
  }

  private async passPair(gate: RpgGate, evidence: RpgGateEvidence) {
    await this.ensureBegin(gate);
    await this.pass(gate, evidence);
  }

  private async ensureBegin(gate: RpgGate) {
    const status = this.gateStatus(gate);
    if (status === "PASSED") {return;}
    if (status === "FAILED") {throw new Error("RPG_RUNTIME_PROTOCOL_VIOLATION");}
    this.activeGate = gate;
    if (status === "IN_PROGRESS") {return;}
    this.updateGate(gate, "IN_PROGRESS");
    await this.gates.begin(gate);
    this.recordAcceptedGate(gate, "IN_PROGRESS", null);
  }

  private async pass(gate: RpgGate, evidence: RpgGateEvidence) {
    await this.gates.pass(gate, evidence);
    this.activeGate = null;
    this.recordAcceptedGate(gate, "PASSED", evidence);
  }

  private async failActiveGate(error: unknown) {
    const gate = this.activeGate;
    if (gate && !this.signal.aborted) {
      try {await this.gates.fail(gate); this.recordAcceptedGate(gate, "FAILED", {});}
      catch { /* Preserve the original validation failure when the terminal report cannot be delivered. */ }
    }
    this.activeGate = null;
    this.setFatal(error);
  }

  private isRestore() {return this.config.checkpoint !== null;}

  private gateStatus(gate: RpgGate) {return this.snapshot.gates[gate];}

  private gatePassed(gate: RpgGate) {return this.gateStatus(gate) === "PASSED";}

  private requireInstance() {
    if (!this.instance) {throw new Error("RPG_RUNTIME_NOT_READY");}
    return this.instance;
  }

  private requireRuntime() {
    if (!this.runtime) {throw new Error("RPG_RUNTIME_NOT_READY");}
    return this.runtime;
  }

  private updateGate(gate: RpgGate, status: RpgValidationGateStatus) {
    this.patch({
      gates: { ...this.snapshot.gates, [gate]: status },
      machineGates: projectMachineGate(this.snapshot.machineGates, gate, status, null, false),
    });
  }

  private recordAcceptedGate(gate: RpgGate, status: RpgValidationGateStatus, evidence: RpgGateEvidence | null) {
    this.patch({
      gates: { ...this.snapshot.gates, [gate]: status },
      lastGateSequence: this.snapshot.lastGateSequence + 1,
      validationState: projectValidationState(this.snapshot.validationState, gate, status),
      machineGates: projectMachineGate(this.snapshot.machineGates, gate, status, evidence, true),
      ...projectPositionEvidence(gate, evidence),
    });
  }

  private setPhase(phase: RpgValidationPhase, message: string, actionLabel: string | null) {
    this.patch({ phase, title: phaseTitle(phase), message, actionLabel, busy: false, error: null });
  }

  private setFatal(error: unknown) {
    this.patch({ phase: "error", title: "运行验证失败", message: "当前 validation 已停止，返回审核页查看机器 gate。", actionLabel: null, busy: false, error: errorMessage(error) });
  }

  private patch(change: Partial<RpgValidationSnapshot>) {
    this.snapshot = { ...this.snapshot, ...change };
    for (const listener of this.listeners) {listener();}
  }
}

function requireValidationResume(config: RpgMakerConfig) {
  const resume = validValidationResume(config);
  if (!resume) {throw new Error("RPG_RUNTIME_PROTOCOL_VIOLATION");}
  return resume;
}

function phaseTitle(phase: RpgValidationPhase) {
  if (phase === "input") {return "验证输入";}
  if (phase === "audio") {return "验证音频";}
  if (phase === "save") {return "记录保存点 B";}
  if (phase === "diverge") {return "继续到不同状态 C";}
  if (phase === "finish") {return "结束原运行";}
  if (phase === "original-complete") {return "原运行验证完成";}
  if (phase === "restore-input") {return "验证恢复后输入";}
  if (phase === "restore-complete") {return "恢复验证完成";}
  return phase === "error" ? "运行验证失败" : "正在执行自动检查";
}

function readPosition(runtime: GameRuntime) {
  const probe = runtime.getValidationProbe(rpgMakerPositionProbeKind);
  const position = probe?.value as RpgPosition | undefined;
  if (!probe || probe.kind !== rpgMakerPositionProbeKind || probe.schemaVersion !== 1 ||
      !position || !validateRpgPosition(position)) {
    throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");
  }
  return { ...position };
}

export async function waitForRpgPosition(
  runtime: GameRuntime,
  signal: AbortSignal,
  wait: (signal: AbortSignal) => Promise<void> = waitForPositionSample,
) {
  const deadline = performance.now() + 120_000;
  while (performance.now() < deadline) {
    try {return readPosition(runtime);}
    catch {await wait(signal);}
  }
  throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");
}

export async function waitForContinuousFrames(
  runtime: GameRuntime,
  signal: AbortSignal,
  wait: (signal: AbortSignal) => Promise<void> = waitForFrameSample,
) {
  await waitForRpgPosition(runtime, signal, wait);
  const deadline = performance.now() + 30_000;
  const first = await readFrameWhenAvailable(runtime, signal, deadline, wait);
  let previous = first;
  while (performance.now() < deadline) {
    await wait(signal);
    const current = await readFrameWhenAvailable(runtime, signal, deadline, wait);
    if (current < previous) {throw new Error("RPG_RUNTIME_FRAME_DISCONTINUITY");}
    const continuous = current - first;
    if (continuous >= 300) {
      if (continuous > 36_000) {throw new Error("RPG_RUNTIME_FRAME_EVIDENCE_INVALID");}
      return continuous;
    }
    previous = current;
  }
  throw new Error("RPG_RUNTIME_TIMEOUT");
}

async function readFrameWhenAvailable(
  runtime: GameRuntime,
  signal: AbortSignal,
  deadline: number,
  wait: (signal: AbortSignal) => Promise<void>,
) {
  while (performance.now() < deadline) {
    try {return readFrame(runtime);}
    catch (error) {
      if (!transientFrameReadError(error)) {throw error;}
      await wait(signal);
    }
  }
  throw new Error("RPG_RUNTIME_TIMEOUT");
}

function transientFrameReadError(error: unknown) {
  return error instanceof Error &&
    (error.message === "RPG_RUNTIME_POSITION_UNAVAILABLE" || error.message === "RPG_RUNTIME_FRAME_UNAVAILABLE");
}

function readFrame(runtime: GameRuntime) {
  const frame = runtime.getFrameCount();
  if (!Number.isSafeInteger(frame) || Number(frame) < 0) {throw new Error("RPG_RUNTIME_FRAME_UNAVAILABLE");}
  return Number(frame);
}

function waitForFrameSample(signal: AbortSignal) {
  return waitForDelay(signal, 50);
}

function waitForPositionSample(signal: AbortSignal) {
  return waitForDelay(signal, 100);
}

function waitForDelay(signal: AbortSignal, delayMs: number) {
  return new Promise<void>((resolve, reject) => {
    if (signal.aborted) {reject(new DOMException("Aborted", "AbortError")); return;}
    const finish = () => {
      signal.removeEventListener("abort", abort);
      resolve();
    };
    const abort = () => {
      window.clearTimeout(timer);
      reject(new DOMException("Aborted", "AbortError"));
    };
    const timer = window.setTimeout(finish, delayMs);
    signal.addEventListener("abort", abort, { once: true });
  });
}

async function checkpointEvidence(payload: ManualStatePayload, receipt: ValidationCheckpointReceipt) {
  const digest = await sha256(payload.state);
  const local = {
    payloadKind: payload.payloadKind ?? "RUNTIME_STATE",
    sizeBytes: payload.state.byteLength,
    sha256: [...digest].map((value) => value.toString(16).padStart(2, "0")).join(""),
  };
  if (receipt.payloadKind !== local.payloadKind || receipt.sizeBytes !== local.sizeBytes || receipt.sha256 !== local.sha256) {
    throw new Error("RPG_CHECKPOINT_RESPONSE_MISMATCH");
  }
  return receipt satisfies RpgGateEvidence;
}

async function uploadRestoreScreenshot(launchId: string, instance: EmulatorInstance, signal: AbortSignal) {
  const screenshot = await captureManualScreenshot(instance);
  if (screenshot.format.toLowerCase() !== "png" || screenshot.screenshot.size <= 0 || screenshot.screenshot.size > 10 * 1024 * 1024) {
    throw new Error("RPG_RUNTIME_SCREENSHOT_INVALID");
  }
  const response = await fetch(`/runtime/launches/${launchId}/review-screenshot`, {
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
    headers: { "Content-Type": "image/png" },
    body: screenshot.screenshot,
    signal,
  });
  if (!response.ok) {throw new Error(`RPG_RUNTIME_SCREENSHOT_HTTP_${response.status}`);}
}

function checkpointUnavailableMessage(reason: string | null | undefined) {
  return reason ? `当前位置不能创建检查点（${reason}），请回到可保存地图后重试。` : "当前位置不能创建检查点，请回到可保存地图后重试。";
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "RPG_RUNTIME_VALIDATION_FAILED";
}
