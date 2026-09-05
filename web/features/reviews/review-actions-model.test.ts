import {describe, expect, it} from "vitest";
import {reviewReadiness, reviewReadyForPublish, type ReviewWorkspace} from "./review-actions-model";

describe("review readiness", () => {
  it("keeps current server dependency checks authoritative for every engine", () => {
    expect(reviewReadiness("READY", null, true, undefined, undefined, true).publishReady).toBe(true);
    expect(reviewReadiness("READY", null, false, undefined, undefined, true).publishReady).toBe(false);
    expect(reviewReadiness("BLOCKED", null, false, undefined, undefined, true).publishReady).toBe(false);
  });
  it("does not let screenshots bypass missing RPG dependencies or active attachment work", () => {
    const screenshot = {} as NonNullable<ReviewWorkspace["runtimeScreenshot"]>;
    expect(reviewReadiness("BLOCKED", screenshot, false, undefined, undefined, true)).toMatchObject({
      publishReady: false, screenshotOverride: false,
    });
    expect(reviewReadyForPublish({
      canApprove: true, arcadeDependencies: {activeAttachment: {state: "RUNNING"}},
    } as ReviewWorkspace)).toBe(false);
  });
});
