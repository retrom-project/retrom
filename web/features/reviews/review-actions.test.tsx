import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Link from "next/link";
import { ReviewActions, type ReviewWorkspace } from "./review-actions";

const router = vi.hoisted(() => ({ replace: vi.fn(), refresh: vi.fn(), push: vi.fn() }));

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
    router.push.mockReset();
    sessionStorage.clear();
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("waits for a Hasheous job and opens one editable comparison dialog", async () => {
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
        assets: [{ candidateAssetId: "cover-1", kind: "COVER", ordinal: 0, status: "READY", widthPx: 320, heightPx: 480, mediaType: "image/png", errorCode: null }],
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
    const dialog = await screen.findByRole("alertdialog", { name: "对比最新查询结果" });
    const latestTitle = within(dialog).getByLabelText("标题");
    expect(latestTitle).toHaveValue("1941: Counter Attack");
    expect(latestTitle.closest("label")).toHaveClass("is-changed");
    await user.click(within(dialog).getByRole("button", { name: "应用到草稿" }));
    expect(screen.getByLabelText("标题")).toHaveValue("1941: Counter Attack");
    expect(screen.getByText(/已应用到草稿/)).toBeInTheDocument();
    expect(router.refresh).not.toHaveBeenCalled();
  });

  it("auto-fills a successful first candidate into an unsaved draft", () => {
    render(<ReviewActions review={{ ...review, candidates: [{ candidateId: "candidate-first", scrapeRunId: "run-first", providerGameId: "42", metadata: { title: "Scraped title", publisher: "Publisher" }, evidence: {}, assets: [] }] }} />);
    expect(screen.getByLabelText("标题")).toHaveValue("Scraped title");
    expect(screen.getByLabelText("发行商")).toHaveValue("Publisher");
    expect(screen.getByText(/首次查询到的游戏信息/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "通过并发布" })).toBeDisabled();
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

  it("uses a three-choice application dialog for unsaved in-app navigation", async () => {
    const user = userEvent.setup();
    render(<><Link href="/admin/reviews">返回列表</Link><ReviewActions review={review} /></>);

    await user.clear(screen.getByLabelText("标题"));
    await user.type(screen.getByLabelText("标题"), "Changed");
    await user.click(screen.getByRole("link", { name: "返回列表" }));

    const dialog = screen.getByRole("alertdialog", { name: "草稿还没有保存" });
    expect(dialog).toHaveTextContent("保存并离开");
    expect(dialog).toHaveTextContent("放弃修改");
    expect(dialog).toHaveTextContent("留在页面");
    await user.click(screen.getByRole("button", { name: "留在页面" }));
    expect(router.push).not.toHaveBeenCalled();

    await user.click(screen.getByRole("link", { name: "返回列表" }));
    await user.click(screen.getByRole("button", { name: "放弃修改" }));
    expect(router.push).toHaveBeenCalledWith("/admin/reviews");
  });
});
