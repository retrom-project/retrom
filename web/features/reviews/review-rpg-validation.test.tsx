import { cleanup, render, screen } from "@testing-library/react";
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
      { gate: "ENGINE_PROFILE", status: "PASSED", begunAtMs: 1, completedAtMs: 2, evidence: { generation: "RPGMZ", adapterId: "rpg-native-web-v5", engineProfile: "mz-v1" }, failureCode: null },
      { gate: "FRAMES_300", status: "PASSED", begunAtMs: 3, completedAtMs: 4, evidence: { continuousFrames: 360 }, failureCode: null },
      { gate: "SAVE_POINT_RECORDED", status: "PASSED", begunAtMs: 5, completedAtMs: 6, evidence: { mapId: 1, playerX: 8, playerY: 7, fixtureState: 1 }, failureCode: null },
      { gate: "CHECKPOINT_CREATED", status: "PASSED", begunAtMs: 7, completedAtMs: 8, evidence: { payloadKind: "NATIVE_SAVE_BUNDLE_V1", sizeBytes: 4096, sha256: "a".repeat(64) }, failureCode: null },
    ],
    failureCode: null,
    expiresAtMs: 10,
    routeEvidence: { coreId: "rpgmaker_mz", generation: "RPGMZ", evidenceGeneration: "RPGMZ", evidenceConfidence: "MATCHED", routeKey: "RPGMZ_NATIVE_V7", adapterId: "rpg-native-web-v5", adapterAbi: "rpg-native-save-v1" },
    checkpointRoundTrip: { created: true, payloadKind: "NATIVE_SAVE_BUNDLE_V1", resumeSlot: 1, sizeBytes: 4096, sha256: "a".repeat(64), originalLaunchId: "10000000-0000-4000-8000-000000000003", initialPosition: { mapId: 1, playerX: 4, playerY: 7, fixtureState: 0 }, savedPosition: { mapId: 1, playerX: 8, playerY: 7, fixtureState: 1 }, divergedPosition: { mapId: 1, playerX: 11, playerY: 7, fixtureState: 2 }, originalLaunchEnded: true, restoreLaunchId: "10000000-0000-4000-8000-000000000004", restoreStarted: true, restoredPosition: { mapId: 1, playerX: 8, playerY: 7, fixtureState: 1 }, positionVerified: true, screenshotUrl: "/screenshot", restoreInputPosition: { mapId: 1, playerX: 9, playerY: 7, fixtureState: 1 }, restoreInputVerified: true },
    decision: null,
  },
};

describe("RPGValidationCard", () => {
  it("renders server launch identity, sequence and gate evidence", () => {
    render(<RPGValidationCard value={review} disabled={false} onChange={vi.fn()} />);

    expect(screen.getByText("10000000-0000-4000-8000-000000000003")).toBeVisible();
    expect(screen.getByText("10000000-0000-4000-8000-000000000004")).toBeVisible();
    expect(screen.getByText("服务端 gate 序号：28")).toBeVisible();
    expect(screen.getByText("RPGMZ · mz-v1 · rpg-native-web-v5")).toBeVisible();
    expect(screen.getByText("连续帧 360")).toBeVisible();
    expect(screen.getAllByText("地图 1 · (8, 7) · 状态 1").length).toBeGreaterThan(0);
    expect(screen.getByText(/NATIVE_SAVE_BUNDLE_V1 · 4 KB · SHA-256 a{12}/)).toBeVisible();
  });
});
