import {cleanup, render, screen, waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {afterEach, expect, it, vi} from "vitest";
import {ReviewActions, type ReviewWorkspace} from "./review-actions";

vi.mock("next/navigation", () => ({useRouter: () => ({refresh: vi.fn(), replace: vi.fn()})}));
vi.mock("@/features/auth/auth-provider", () => ({useAuth: () => ({context: {user: {userId: "user-1"}}})}));
afterEach(() => {cleanup(); vi.unstubAllGlobals();});

it("does not carry a local candidate choice across a refreshed source snapshot", async () => {
  const review: ReviewWorkspace = {
    itemId: "item-1", version: 1, canApprove: false, effectiveSourceSnapshotId: "source-1",
    metadata: {title: "Collection", description: "", developer: "", publisher: "", genre: "", players: null, releaseYear: null},
    candidates: [], selectedCandidateId: null, defaultDosEntry: null, dosEntries: [],
    selectedAssets: {coverCandidateAssetId: null, backgroundCandidateAssetId: null, screenshotCandidateAssetIds: []},
    validation: {id: "validation-1", status: "BLOCKED", compatibilityCode: "SCUMMVM_SELECTION_REQUIRED",
      dependencySnapshot: {kind: "SCUMMVM", selectedCandidateId: "", detection: {candidates: [
        {id: "first", root: "One", engineId: "sky", gameId: "sky", description: "First game", language: "en", platform: "pc", extra: "", blocker: ""},
        {id: "second", root: "Two", engineId: "sky", gameId: "sky", description: "Second game", language: "en", platform: "pc", extra: "", blocker: ""},
      ]}}},
  };
  const writes: Array<{scummvmCandidateId?: string}> = [];
  const fetcher = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
    if (init?.method === "PATCH") {writes.push(JSON.parse(String(init.body))); return Response.json({version: 2});}
    return Response.json({...review, version: 2, effectiveSourceSnapshotId: "source-2"});
  });
  vi.stubGlobal("fetch", fetcher);
  render(<ReviewActions review={review} />);
  await userEvent.setup().selectOptions(screen.getByRole("combobox", {name: "运行版本"}), "second");
  await waitFor(() => expect(writes[0]).toMatchObject({scummvmCandidateId: "second"}));
  await waitFor(() => expect(screen.getByRole("combobox", {name: "运行版本"})).toHaveValue(""));
  expect(screen.getByRole("button", {name: "通过并发布"})).toBeDisabled();
});
