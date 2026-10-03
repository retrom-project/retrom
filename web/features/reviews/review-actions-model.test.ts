import {describe, expect, it} from "vitest";
import {draftPatchPayload, reviewReadiness, reviewReadyForPublish, type DraftPayload, type ReviewWorkspace} from "./review-actions-model";

it("patches independent metadata edits without resubmitting an untouched title and preserves explicit clears", () => {
  const metadata = {title: "", description: "Before", developer: "", publisher: "", genre: "", players: 2, releaseYear: 2000};
  const payload: DraftPayload = {metadata: {...metadata, description: "After", players: null},
    selectedCandidateId: null, selectedAssets: {coverCandidateAssetId: null, coverUploadedAssetId: null,
      videoUploadedAssetId: null, backgroundCandidateAssetId: null, screenshotCandidateAssetIds: []},
    defaultDosEntry: null, tagIds: []};
  expect(draftPatchPayload(payload, metadata).metadata).toEqual({description: "After", players: null});
  expect(draftPatchPayload({...payload, metadata: {...payload.metadata, title: "Chosen title"}}, payload.metadata).metadata)
    .toEqual({title: "Chosen title"});
});

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
