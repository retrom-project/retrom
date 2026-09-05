import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ReviewActions, type ReviewWorkspace } from "./review-actions";

const router = vi.hoisted(() => ({ replace: vi.fn(), refresh: vi.fn(), push: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "user-1" } } }) }));

const review: ReviewWorkspace = {
  itemId: "item-1", version: 1, canApprove: false,
  platformInstance: { id: "rpg-directory", name: "RPG Maker MV" },
  metadata: { title: "Manual", description: "", developer: "", publisher: "", genre: "", players: null, releaseYear: null },
  validation: { id: "static-validation", status: "BLOCKED", compatibilityCode: "RPG_RUNTIME_PACK_MISSING" },
  candidates: [], uploadedAssets: [], scrapeRuns: [], selectedCandidateId: null,
  selectedAssets: { coverCandidateAssetId: null, coverUploadedAssetId: null, backgroundCandidateAssetId: null, screenshotCandidateAssetIds: [] },
  defaultDosEntry: null, dosEntries: [],
  rpgMaker: {
    selectedCoreId: "rpgmaker", generation: "RPGMV", evidenceGeneration: "RPGMV",
    evidenceConfidence: "MATCHED", selfContained: true, selfContainedOverride: false,
    runtimePackRequirements: [], runtimePackSelections: [],
  },
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("ordinary RPG review", () => {
  it.each([
    ["RPG2000", "RPG Maker 2000"], ["RPG2003", "RPG Maker 2003"], ["RPGXP", "RPG Maker XP"],
    ["RPGVX", "RPG Maker VX"], ["RPGVXACE", "RPG Maker VX Ace"], ["RPGMV", "RPG Maker MV"], ["RPGMZ", "RPG Maker MZ"],
  ])("shows the detected %s generation even though every target shares the virtual core", (generation, label) => {
    render(<ReviewActions review={{...review, rpgMaker: {...review.rpgMaker!, generation}}} />);
    expect(screen.getByText(label, {selector: ".review-rpg-facts strong"})).toBeInTheDocument();
    expect(screen.queryByText("rpgmaker", {selector: ".review-rpg-facts strong"})).not.toBeInTheDocument();
  });
  it("keeps real dependency failures blocking publication but leaves best-effort trial available", () => {
    render(<ReviewActions review={review} />);
    expect(screen.getByRole("button", {name: "通过并发布"})).toBeDisabled();
    expect(screen.getByRole("button", {name: "运行游戏"})).toBeEnabled();
    expect(screen.queryByText("高级验证详情")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", {name: "重新运行检查"})).not.toBeInTheDocument();
  });
  it("allows current ready content to publish without a trial, gate polling or another decision", async () => {
    vi.useFakeTimers();
    try {
      const fetchMock = vi.fn();
      vi.stubGlobal("fetch", fetchMock);
      render(<ReviewActions review={{...review, canApprove: true,
        validation: {...review.validation!, status: "READY", compatibilityCode: "READY"}}} />);
      expect(screen.getByRole("button", {name: "通过并发布"})).toBeEnabled();
      expect(screen.getByRole("button", {name: "运行游戏"})).toBeEnabled();
      await act(() => vi.advanceTimersByTimeAsync(3_000));
      expect(fetchMock).not.toHaveBeenCalled();
    } finally {vi.useRealTimers();}
  });
  it("uses the ordinary preview endpoint for RPG and allows another independent trial", async () => {
    const popup = {closed: false, close: vi.fn(), location: {replace: vi.fn()},
      document: {title: "", body: {style: {}, textContent: ""}}} as unknown as Window;
    vi.spyOn(window, "open").mockReturnValue(popup);
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      expect(String(input)).toBe("/api/v1/admin/reviews/item-1/previews");
      expect(init?.method).toBe("POST");
      return Promise.resolve(jsonResponse({previewId: "preview-1", playUrl: "/admin/review-previews/preview-1"}, 201));
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={review} />);
    await user.click(screen.getByRole("button", {name: "运行游戏"}));
    await waitFor(() => expect(popup.location.replace).toHaveBeenCalledWith("/admin/review-previews/preview-1"));
    expect(screen.getByRole("button", {name: "运行游戏"})).toBeEnabled();
    expect(screen.getByRole("button", {name: "通过并发布"})).toBeDisabled();
    await user.click(screen.getByRole("button", {name: "运行游戏"}));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });
});
