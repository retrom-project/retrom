import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RPGMakerReview } from "./review-actions-model";
import { RPGValidationCard } from "./review-rpg-validation";

afterEach(cleanup);

const review: RPGMakerReview = {
  selectedCoreId: "rpgmaker_mz",
  generation: "RPGMZ",
  evidenceGeneration: "RPGMZ",
  evidenceConfidence: "MATCHED",
  selfContained: true,
  selfContainedOverride: false,
  runtimeBindingRevision: 3,
  runtimePackRequirements: [],
  runtimePackSelections: [],
  runtimeValidationCurrent: true,
  runtimeValidation: {
    validationId: "10000000-0000-4000-8000-000000000001",
    importItemId: "10000000-0000-4000-8000-000000000002",
    reviewVersionAtCreate: 4,
    runtimeBindingRevision: 3,
    launchId: "10000000-0000-4000-8000-000000000003",
    restoreLaunchId: "10000000-0000-4000-8000-000000000004",
    state: "AWAITING_DECISION",
    lastGateSequence: 28,
    machineGates: [
      { gate: "ENGINE_PROFILE", status: "PASSED", begunAtMs: 1, completedAtMs: 2, evidence: { generation: "RPGMZ", engineProfile: "RPGMZ" }, failureCode: null },
      { gate: "FRAMES_300", status: "PASSED", begunAtMs: 3, completedAtMs: 4, evidence: { continuousFrames: 360 }, failureCode: null },
      { gate: "SAVE_POINT_RECORDED", status: "PASSED", begunAtMs: 5, completedAtMs: 6, evidence: { mapId: 1, playerX: 8, playerY: 7, fixtureState: 1 }, failureCode: null },
      { gate: "CHECKPOINT_CREATED", status: "PASSED", begunAtMs: 7, completedAtMs: 8, evidence: { checkpointFormat: "rpgmaker-native-save-v1", sizeBytes: 4096, sha256: "a".repeat(64) }, failureCode: null },
    ],
    failureCode: null,
    expiresAtMs: 10,
    routeEvidence: { effectiveSourceSnapshotId: "10000000-0000-4000-8000-000000000005", generation: "RPGMZ", evidenceGeneration: "RPGMZ", evidenceConfidence: "MATCHED", providerId: "retrom-runtime", targetId: "rpgmaker-mz", gameCompatibilityLine: "rpgmaker-mz-v1", targetContractSha256: "b".repeat(64), dependencySnapshotSha256: "c".repeat(64), projectFingerprint: "d".repeat(64) },
    checkpointRoundTrip: { created: true, checkpointFormat: "rpgmaker-native-save-v1", sizeBytes: 4096, sha256: "a".repeat(64), originalLaunchId: "10000000-0000-4000-8000-000000000003", initialPosition: { mapId: 1, playerX: 4, playerY: 7, fixtureState: 0 }, savedPosition: { mapId: 1, playerX: 8, playerY: 7, fixtureState: 1 }, divergedPosition: { mapId: 1, playerX: 11, playerY: 7, fixtureState: 2 }, originalLaunchEnded: true, restoreLaunchId: "10000000-0000-4000-8000-000000000004", restoreStarted: true, restoredPosition: { mapId: 1, playerX: 8, playerY: 7, fixtureState: 1 }, positionVerified: true, screenshotUrl: "/api/v1/admin/review-assets/shot", restoreInputPosition: { mapId: 1, playerX: 9, playerY: 7, fixtureState: 1 }, restoreInputVerified: true },
    createdAtMs: 0, updatedAtMs: 9,
    decision: null,
  },
};

describe("RPGValidationCard", () => {
  it("keeps advanced server evidence collapsed until the reviewer asks for it", async () => {
    const user = userEvent.setup();
    render(<RPGValidationCard value={review} disabled={false} onChange={vi.fn()} />);

    expect(screen.getByText("高级验证详情")).toBeVisible();
    expect(screen.getByText("服务端进度 28 / 28 · 已完成")).toBeVisible();
    expect(screen.getByText("10000000-0000-4000-8000-000000000003")).not.toBeVisible();
    expect(screen.getByText("连续帧 360")).not.toBeVisible();

    await user.click(screen.getByText("高级验证详情"));

    expect(screen.getByText("10000000-0000-4000-8000-000000000003")).toBeVisible();
    expect(screen.getByText("10000000-0000-4000-8000-000000000004")).toBeVisible();
    expect(screen.getByText("服务端 gate 序号：28")).toBeVisible();
    expect(screen.getByText("RPGMZ · RPGMZ")).toBeVisible();
    expect(screen.getByText("连续帧 360")).toBeVisible();
    expect(screen.getAllByText("地图 1 · (8, 7) · 状态 1").length).toBeGreaterThan(0);
    expect(screen.getByText(/rpgmaker-native-save-v1 · 4 KB · SHA-256 a{12}/)).toBeVisible();
  });

  it("surfaces a failed result in the compact summary without expanding the evidence", () => {
    render(<RPGValidationCard value={{
      ...review,
      runtimeValidation: { ...review.runtimeValidation!, state: "FAILED", failureCode: "RPG_RUNTIME_PROTOCOL_VIOLATION" },
    }} disabled={false} onChange={vi.fn()} />);

    expect(screen.getByText("验证失败 · RPG_RUNTIME_PROTOCOL_VIOLATION")).toBeVisible();
    expect(screen.getByText("连续帧 360")).not.toBeVisible();
  });

  it("does not expose the internal running state as a redundant header badge", () => {
    render(<RPGValidationCard value={{
      ...review,
      runtimeValidation: { ...review.runtimeValidation!, state: "RUNNING", lastGateSequence: 6 },
    }} disabled={false} onChange={vi.fn()} />);

    expect(screen.queryByText("RUNNING")).not.toBeInTheDocument();
    expect(screen.getByText("服务端进度 6 / 28 · 进行中")).toBeVisible();
  });

  it("keeps the launch guidance compact before validation starts", () => {
    render(<RPGValidationCard value={{ ...review, runtimeValidation: null }} disabled={false} onChange={vi.fn()} />);

    expect(screen.getByText(/点击“运行游戏”后即可发布/)).toBeVisible();
    expect(screen.queryByText("高级验证详情")).not.toBeInTheDocument();
  });
});
