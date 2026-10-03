import {describe, expect, it} from "vitest";
import {reviewReadiness, reviewReadyForPublish, type ReviewWorkspace} from "./review-actions-model";

describe("review readiness", () => {
  it("keeps current server dependency checks authoritative for every engine", () => {
    expect(reviewReadiness("READY", null, true, undefined, undefined).publishReady).toBe(true);
    expect(reviewReadiness("READY", null, false, undefined, undefined).publishReady).toBe(false);
    expect(reviewReadiness("BLOCKED", null, false, undefined, undefined).publishReady).toBe(false);
  });
  it("uses server permission for screenshot approval and waits for active attachments", () => {
    const screenshot = {} as NonNullable<ReviewWorkspace["runtimeScreenshot"]>;
    expect(reviewReadiness("BLOCKED", screenshot, false, undefined, undefined)).toMatchObject({
      publishReady: false, screenshotOverride: false,
    });
    expect(reviewReadiness("BLOCKED", screenshot, true, undefined, undefined)).toMatchObject({publishReady: true, screenshotOverride: true});
    expect(reviewReadyForPublish({
      canApprove: true, arcadeDependencies: {activeAttachment: {state: "RUNNING"}},
    } as ReviewWorkspace)).toBe(false);
  });
});
