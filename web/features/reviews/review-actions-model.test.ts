import {describe, expect, it} from "vitest";

import {rpgReviewRuntimeStatus, type RPGMakerReview} from "./review-actions-model";

function rpgMaker(state: "EXPIRED" | "STARTING", launchId: string | null): RPGMakerReview {
  return {
    selectedCoreId: "rpgmaker_2003", generation: "RPG2003", evidenceGeneration: "RPG2003",
    evidenceConfidence: "MATCHED", selfContained: true, selfContainedOverride: false,
    runtimePackRequirements: [], runtimePackSelections: [], runtimeValidationCurrent: true,
    runtimeValidation: {
      validationId: "validation-1", importItemId: "item-1", reviewVersionAtCreate: 1,
      launchId, restoreLaunchId: null, state, lastGateSequence: 0, machineGates: [], failureCode: null,
      expiresAtMs: Date.now() + 60_000,
      routeEvidence: {
        effectiveSourceSnapshotId: "10000000-0000-4000-8000-000000000001",
        generation: "RPG2003", evidenceGeneration: "RPG2003", evidenceConfidence: "MATCHED",
        providerId: "retrom-runtime", targetId: "rpgmaker-2003",
        dependencySnapshotSha256: "c".repeat(64), projectFingerprint: "d".repeat(64),
      },
      checkpointRoundTrip: {
        created: false, checkpointFormat: null, sizeBytes: null, sha256: null,
        originalLaunchId: launchId, initialPosition: null, savedPosition: null, divergedPosition: null,
        originalLaunchEnded: false, restoreLaunchId: null, restoreStarted: false,
        restoredPosition: null, positionVerified: false, screenshotUrl: null,
        restoreInputPosition: null, restoreInputVerified: false,
      },
      createdAtMs: 0, updatedAtMs: 0, decision: null,
    },
  };
}

describe("RPG Maker review runtime status", () => {
  it("treats an expired optional evidence window with a current Launch as publishable", () => {
    expect(rpgReviewRuntimeStatus(rpgMaker("EXPIRED", "launch-1"))).toEqual({
      compatibilityCode: "READY", compatibilityLabel: "已启动游戏，可发布", status: "READY",
    });
  });

  it("does not accept a Launch bound to superseded runtime inputs", () => {
    expect(rpgReviewRuntimeStatus({...rpgMaker("STARTING", "launch-1"), runtimeValidationCurrent: false})).toEqual({
      compatibilityCode: "NEEDS_VALIDATION", compatibilityLabel: "等待启动游戏", status: "PENDING",
    });
  });
});
