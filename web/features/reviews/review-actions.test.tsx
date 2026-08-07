import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ReviewActions, type ReviewWorkspace } from "./review-actions";

const router = vi.hoisted(() => ({ replace: vi.fn(), refresh: vi.fn() }));

vi.mock("next/navigation", () => ({ useRouter: () => router }));

const review: ReviewWorkspace = {
  itemId: "item-1",
  version: 1,
  metadata: { title: "Manual", description: "", developer: "", publisher: "", genre: "", players: null, releaseYear: null },
  validation: { id: "validation-1", status: "READY", compatibilityCode: "READY" },
  candidates: [],
  scrapeRuns: [],
  selectedCandidateId: null,
  selectedAssets: { coverCandidateAssetId: null, backgroundCandidateAssetId: null, screenshotCandidateAssetIds: [] },
  defaultDosEntry: null,
  dosEntries: [],
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("ReviewActions", () => {
  beforeEach(() => {
    router.replace.mockReset();
    router.refresh.mockReset();
    sessionStorage.clear();
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("waits for a Hasheous job, shows progress, and refreshes candidates", async () => {
    let finishJob: ((response: Response) => void) | undefined;
    const jobResponse = new Promise<Response>((resolve) => { finishJob = resolve; });
    const updated: ReviewWorkspace = {
      ...review,
      version: 2,
      candidates: [{
        candidateId: "candidate-1",
        scrapeRunId: "run-1",
        providerGameId: "50192",
        metadata: { title: "1941: Counter Attack", publisher: "Capcom" },
        evidence: {},
        assets: [],
      }],
      scrapeRuns: [{
        scrapeRunId: "run-1", jobId: "job-1", provider: "HASHEOUS", state: "COMPLETED", jobState: "SUCCEEDED",
        createdAtMs: 1_700_000_000_000, completedAtMs: 1_700_000_001_000, errorCode: null,
        evidenceCount: 8, attemptCount: 8, candidateCount: 1,
        outcomes: { hit: 8, miss: 0, rateLimited: 0, timeout: 0, invalidResponse: 0, networkError: 0 },
      }],
    };
    vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/scrape-candidates") && init?.method === "POST") return Promise.resolve(jsonResponse({ version: 2, state: "QUEUED", scrapeRunId: "run-1", jobId: "job-1" }, 202));
      if (url.endsWith("/jobs/job-1")) return jobResponse;
      if (url.endsWith("/reviews/item-1")) return Promise.resolve(jsonResponse(updated));
      throw new Error(`unexpected fetch ${url}`);
    }));
    const user = userEvent.setup();
    render(<ReviewActions review={review} />);

    await user.click(screen.getByRole("button", { name: "重新查询游戏信息" }));
    expect(await screen.findByRole("button", { name: "查询中…" })).toHaveAttribute("aria-busy", "true");
    expect(screen.getByRole("status", { name: "" })).toHaveTextContent("正在查询游戏信息");

    finishJob?.(jsonResponse({ state: "SUCCEEDED" }));
    expect(await screen.findByText("1941: Counter Attack")).toBeInTheDocument();
    expect(await screen.findAllByText(/已刷新 1 个候选/)).toHaveLength(2);
    expect(screen.getByRole("button", { name: "采用候选文本" })).toBeEnabled();
    expect(router.refresh).toHaveBeenCalledOnce();
  });

  it("shows publishing state and replaces the decided route with a flash message", async () => {
    let finishPublish: ((response: Response) => void) | undefined;
    const publishResponse = new Promise<Response>((resolve) => { finishPublish = resolve; });
    vi.stubGlobal("fetch", vi.fn(() => publishResponse));
    const user = userEvent.setup();
    render(<ReviewActions review={review} returnTo="/admin/reviews?importJobId=batch-1" nextItemId="item-2" />);

    await user.click(screen.getByRole("button", { name: "通过并发布" }));
    expect(await screen.findByRole("button", { name: "正在发布…" })).toHaveAttribute("aria-busy", "true");
    finishPublish?.(jsonResponse({ gameId: "game-1" }, 201));

    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/admin/reviews/item-2?returnTo=%2Fadmin%2Freviews%3FimportJobId%3Dbatch-1"));
    expect(router.refresh).not.toHaveBeenCalled();
    expect(sessionStorage.getItem("retrom:flash-toast")).toContain("游戏已成功发布");
  });
});
