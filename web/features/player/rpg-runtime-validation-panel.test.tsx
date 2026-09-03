import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RpgRuntimeValidationPanel } from "./rpg-runtime-validation-panel";
import type { RpgValidationSnapshot } from "./rpg-runtime-validation";
import { rpgValidationGates, type RpgGate, type RpgGateEvidence } from "./rpg-validation-protocol";

const originalLaunchId = "01980000-0000-7000-8000-000000000101";
const restoreLaunchId = "01980000-0000-7000-8000-000000000102";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("RpgRuntimeValidationPanel", () => {
  it("renders the server resume identity, sequence, every gate evidence and all five positions", () => {
    const driver = panelDriver("restore-complete");

    render(<RpgRuntimeValidationPanel driver={driver} />);

    const panel = screen.getByRole("complementary", { name: "RPG Maker 运行验证" });
    expect(within(panel).getAllByText("Restore Launch")).toHaveLength(2);
    expect(within(panel).getByText(originalLaunchId)).toBeVisible();
    expect(within(panel).getByText(restoreLaunchId)).toBeVisible();
    expect(within(panel).getByText("28 / 28")).toBeVisible();
    expect(within(panel).getAllByRole("listitem")).toHaveLength(14);
    expect(gateRow(panel, "ENGINE_PROFILE")).toHaveTextContent("RPGMZ · RPGMZ");
    expect(gateRow(panel, "FRAMES_300")).toHaveTextContent("360 个连续帧");
    expect(gateRow(panel, "CHECKPOINT_CREATED")).toHaveTextContent("rpgmaker-mz-v1");
    expect(gateRow(panel, "RESTORE_SCREENSHOT")).toHaveTextContent("恢复截图已关联");
    expect(gateRow(panel, "RESTORE_INPUT")).toHaveTextContent("地图 1 · X 13 · Y 8 · 变量 1");
    expect(within(panel).getByText("初始 A").nextSibling).toHaveTextContent("地图 1 · X 10 · Y 8 · 变量 0");
    expect(within(panel).getByText("保存 B").nextSibling).toHaveTextContent("地图 1 · X 11 · Y 8 · 变量 1");
    expect(within(panel).getByText("继续 C").nextSibling).toHaveTextContent("地图 1 · X 12 · Y 8 · 变量 2");
    expect(within(panel).getByText("恢复到 B").nextSibling).toHaveTextContent("地图 1 · X 11 · Y 8 · 变量 1");
    const restoreInputTerm = within(panel).getAllByText("恢复后输入").find((element) => element.tagName === "DT");
    expect(restoreInputTerm?.nextSibling).toHaveTextContent("地图 1 · X 13 · Y 8 · 变量 1");
  });

  it("offers the review return only after the restore input has completed", () => {
    const close = vi.spyOn(window, "close").mockImplementation(() => undefined);
    const completed = panelDriver("restore-complete");
    const view = render(<RpgRuntimeValidationPanel driver={completed} />);

    fireEvent.click(screen.getByRole("button", { name: "返回审核决定" }));
    expect(close).toHaveBeenCalledOnce();

    view.unmount();
    render(<RpgRuntimeValidationPanel driver={panelDriver("restore-complete", "RESTORED")} />);
    expect(screen.queryByRole("button", { name: "返回审核决定" })).not.toBeInTheDocument();
  });
});

function gateRow(panel: HTMLElement, gate: RpgGate) {
  const row = within(panel).getByText(gate).closest("li");
  if (!row) {throw new Error(`Missing row for ${gate}`);}
  return row;
}

function panelDriver(
  phase: "restore-input" | "restore-complete",
  validationState: "AWAITING_DECISION" | "RESTORED" = phase === "restore-complete" ? "AWAITING_DECISION" : "RESTORED",
) {
  const positions = {
    INITIAL_POSITION_RECORDED: { mapId: 1, playerX: 10, playerY: 8, fixtureState: 0 },
    SAVE_POINT_RECORDED: { mapId: 1, playerX: 11, playerY: 8, fixtureState: 1 },
    POST_SAVE_STATE_DIVERGED: { mapId: 1, playerX: 12, playerY: 8, fixtureState: 2 },
    RESTORE_POSITION_VERIFIED: { mapId: 1, playerX: 11, playerY: 8, fixtureState: 1 },
    RESTORE_INPUT: { mapId: 1, playerX: 13, playerY: 8, fixtureState: 1 },
  } as const;
  const evidence = (gate: RpgGate): RpgGateEvidence => {
    if (gate in positions) {return positions[gate as keyof typeof positions];}
    if (gate === "ENGINE_PROFILE") {return {generation: "RPGMZ", engineProfile: "RPGMZ"};}
    if (gate === "FRAMES_300") {return { continuousFrames: 360 };}
    if (gate === "INPUT" || gate === "AUDIO") {return { observed: true };}
    if (gate === "CHECKPOINT_CREATED") {
      return {checkpointFormat: "rpgmaker-mz-v1", sizeBytes: 47_816, sha256: "a".repeat(64)};
    }
    return {};
  };
  const machineGates = rpgValidationGates.map((gate) => ({
    gate,
    status: gate === "RESTORE_INPUT" && phase === "restore-input" ? "NOT_STARTED" as const : "PASSED" as const,
    begunAtMs: 1,
    completedAtMs: gate === "RESTORE_INPUT" && phase === "restore-input" ? null : 2,
    evidence: gate === "RESTORE_INPUT" && phase === "restore-input" ? null : evidence(gate),
    failureCode: null,
  }));
  const snapshot = {
    phase,
    title: phase === "restore-complete" ? "恢复验证完成" : "验证恢复后输入",
    message: "服务端机器证据",
    actionLabel: phase === "restore-input" ? "恢复后输入已经生效" : null,
    busy: false,
    error: null,
    gates: gateStatuses((gate) => machineGates.find((candidate) => candidate.gate === gate)?.status ?? "NOT_STARTED"),
    launchRole: "restore" as const,
    originalLaunchId,
    restoreLaunchId,
    validationState,
    lastGateSequence: phase === "restore-complete" ? 28 : 26,
    machineGates,
    initialPosition: positions.INITIAL_POSITION_RECORDED,
    savedPosition: positions.SAVE_POINT_RECORDED,
    divergedPosition: positions.POST_SAVE_STATE_DIVERGED,
    restoredPosition: positions.RESTORE_POSITION_VERIFIED,
    restoreInputPosition: phase === "restore-complete" ? positions.RESTORE_INPUT : null,
    observedPosition: positions.RESTORE_INPUT,
  };
  return {
    subscribe: () => () => undefined,
    getSnapshot: () => snapshot,
    runAction: vi.fn(async () => undefined),
  };
}

function gateStatuses(status: (gate: RpgGate) => RpgValidationSnapshot["gates"][RpgGate]) {
  return {
    RUNTIME_READY: status("RUNTIME_READY"),
    ENGINE_PROFILE: status("ENGINE_PROFILE"),
    FRAMES_300: status("FRAMES_300"),
    INPUT: status("INPUT"),
    AUDIO: status("AUDIO"),
    INITIAL_POSITION_RECORDED: status("INITIAL_POSITION_RECORDED"),
    SAVE_POINT_RECORDED: status("SAVE_POINT_RECORDED"),
    CHECKPOINT_CREATED: status("CHECKPOINT_CREATED"),
    POST_SAVE_STATE_DIVERGED: status("POST_SAVE_STATE_DIVERGED"),
    ORIGINAL_LAUNCH_ENDED: status("ORIGINAL_LAUNCH_ENDED"),
    RESTORE_STARTED: status("RESTORE_STARTED"),
    RESTORE_POSITION_VERIFIED: status("RESTORE_POSITION_VERIFIED"),
    RESTORE_SCREENSHOT: status("RESTORE_SCREENSHOT"),
    RESTORE_INPUT: status("RESTORE_INPUT"),
  };
}
