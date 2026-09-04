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
  validation: { id: "static-validation", status: "BLOCKED", compatibilityCode: "RPG_RUNTIME_VALIDATION_REQUIRED" },
  candidates: [], uploadedAssets: [], scrapeRuns: [], selectedCandidateId: null,
  selectedAssets: { coverCandidateAssetId: null, coverUploadedAssetId: null, backgroundCandidateAssetId: null, screenshotCandidateAssetIds: [] },
  defaultDosEntry: null, dosEntries: [],
  rpgMaker: {
    selectedCoreId: "rpgmaker_mv", generation: "RPGMV", evidenceGeneration: "RPGMV",
    evidenceConfidence: "MATCHED", selfContained: true, selfContainedOverride: false,
    runtimePackRequirements: [], runtimePackSelections: [], runtimeValidation: null,
  },
};

function launchedRPGValidation(): NonNullable<NonNullable<ReviewWorkspace["rpgMaker"]>["runtimeValidation"]> {
  return {
    validationId: "rpg-validation", importItemId: "item-1", reviewVersionAtCreate: 1,
    launchId: "rpg-launch", restoreLaunchId: null, state: "STARTING",
    lastGateSequence: 0, machineGates: [], failureCode: null, expiresAtMs: Date.now() + 60_000,
    routeEvidence: { effectiveSourceSnapshotId: "10000000-0000-4000-8000-000000000001", generation: "RPGMV", evidenceGeneration: "RPGMV", evidenceConfidence: "MATCHED", providerId: "retrom-runtime", targetId: "rpgmaker-mv", dependencySnapshotSha256: "c".repeat(64), projectFingerprint: "d".repeat(64) },
    checkpointRoundTrip: { created: false, checkpointFormat: null, sizeBytes: null, sha256: null, originalLaunchId: "rpg-launch", initialPosition: null, savedPosition: null, divergedPosition: null, originalLaunchEnded: false, restoreLaunchId: null, restoreStarted: false, restoredPosition: null, positionVerified: false, screenshotUrl: null, restoreInputPosition: null, restoreInputVerified: false },
    createdAtMs: 0, updatedAtMs: 0,
    decision: null,
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("ReviewActions RPG runtime validation", () => {
  it("does not expose a runtime deployment recheck action", () => {
    render(<ReviewActions review={review} />);

    expect(screen.queryByRole("button", { name: "重新运行检查" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "运行游戏" })).toBeEnabled();
  });

  it("allows publish after one Launch without polling optional machine gates", async () => {
    vi.useFakeTimers();
    try {
      const fetchMock = vi.fn();
      vi.stubGlobal("fetch", fetchMock);
      render(<ReviewActions review={{
        ...review, canApprove: true,
        rpgMaker: { ...review.rpgMaker!, runtimeValidation: launchedRPGValidation() },
      }} />);

      expect(screen.getByRole("button", { name: "通过并发布" })).toBeEnabled();
      expect(screen.queryByRole("button", { name: "重新运行检查" })).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: "运行游戏" })).toBeDisabled();
      await act(() => vi.advanceTimersByTimeAsync(3_000));
      expect(fetchMock).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps the run action visible and refreshes once after its window closes", async () => {
    const popup = {
      closed: false, close: vi.fn(), location: { replace: vi.fn() },
      document: { title: "", body: { style: { cssText: "" }, textContent: "" } },
    } as unknown as Window;
    vi.spyOn(window, "open").mockReturnValue(popup);
    let validationReads = 0;
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/runtime-validations") && init?.method === "POST") {
        return Promise.resolve(jsonResponse({
          validationId: "rpg-validation", launchId: "rpg-launch", playerUrl: "/play/rpg-launch",
        }, 201));
      }
      if (url.endsWith("/runtime/launches/rpg-launch/finish") && init?.method === "POST") {
        return Promise.resolve(jsonResponse({ state: "FINISHED" }));
      }
      if (url.endsWith("/runtime-validations/rpg-validation") && !init?.method) {
        validationReads += 1;
        const validation = launchedRPGValidation();
        return Promise.resolve(jsonResponse(validationReads === 1 ? validation : {
          ...validation, state: "FAILED", failureCode: "RPG_RUNTIME_VALIDATION_WINDOW_CLOSED",
        }));
      }
      throw new Error(`unexpected fetch ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={review} />);

    await user.click(screen.getByRole("button", { name: "运行游戏" }));
    await waitFor(() => expect(popup.location.replace).toHaveBeenCalledWith("/play/rpg-launch"));
    expect(screen.getByRole("button", { name: "运行游戏" })).toBeDisabled();
    Object.defineProperty(popup, "closed", { configurable: true, value: true });
    await act(() => new Promise((resolve) => window.setTimeout(resolve, 1_100)));
    expect(screen.getByRole("button", { name: "通过并发布" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "运行游戏" })).toBeEnabled();
    expect(fetchMock).toHaveBeenCalledWith("/runtime/launches/rpg-launch/finish", expect.objectContaining({
      method: "POST", credentials: "same-origin",
      body: expect.stringContaining('"clientSequence":0'),
    }));
    expect(fetchMock).toHaveBeenCalledTimes(4);
  });
});
