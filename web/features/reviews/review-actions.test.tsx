import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ReviewActions, type ReviewWorkspace } from "./review-actions";

const router = vi.hoisted(() => ({ replace: vi.fn(), refresh: vi.fn(), push: vi.fn() }));
const upload = vi.hoisted(() => ({ uploadOne: vi.fn(), waitForJob: vi.fn(), waitForJobEvents: vi.fn() }));

vi.mock("next/navigation", () => ({ useRouter: () => router }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "user-1" } } }) }));
vi.mock("@/lib/upload", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/upload")>();
  return { ...original, uploadOne: upload.uploadOne, waitForJob: upload.waitForJob, waitForJobEvents: upload.waitForJobEvents };
});

const review: ReviewWorkspace = {
  itemId: "item-1", version: 1,
  metadata: { title: "Manual", description: "", developer: "", publisher: "", genre: "", players: null, releaseYear: null },
  validation: { id: "validation-1", status: "READY", current: true, compatibilityCode: "READY" },
  candidates: [], uploadedAssets: [], scrapeRuns: [], selectedCandidateId: null,
  selectedAssets: { coverCandidateAssetId: null, coverUploadedAssetId: null, backgroundCandidateAssetId: null, screenshotCandidateAssetIds: [] },
  defaultDosEntry: null, dosEntries: [],
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

describe("ReviewActions", () => {
  beforeEach(() => {
    router.replace.mockReset(); router.refresh.mockReset(); router.push.mockReset();
    upload.uploadOne.mockReset(); upload.waitForJob.mockReset().mockResolvedValue(undefined); upload.waitForJobEvents.mockReset().mockResolvedValue(undefined);
    sessionStorage.clear();
  });

  afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

  it("opens one comparison dialog and autosaves the applied result", async () => {
    const candidate = { candidateId: "candidate-1", scrapeRunId: "run-1", providerGameId: "50192", metadata: { title: "1941: Counter Attack", description: "Long provider description", publisher: "Capcom" }, evidence: {}, assets: [{ candidateAssetId: "cover-1", kind: "COVER" as const, ordinal: 0, status: "READY", widthPx: 320, heightPx: 480, mediaType: "image/png", errorCode: null }] };
    const updated: ReviewWorkspace = { ...review, version: 2, candidates: [candidate], scrapeRuns: [{ scrapeRunId: "run-1", jobId: "job-1", provider: "HASHEOUS", state: "COMPLETED", jobState: "SUCCEEDED", createdAtMs: 1, completedAtMs: 2, errorCode: null, evidenceCount: 1, attemptCount: 1, candidateCount: 1, outcomes: { hit: 1, miss: 0, rateLimited: 0, timeout: 0, invalidResponse: 0, networkError: 0 } }] };
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/scrape-candidates")) return Promise.resolve(jsonResponse({ version: 2, state: "QUEUED", scrapeRunId: "run-1", jobId: "job-1" }, 202));
      if (url.endsWith("/reviews/item-1") && !init?.method) return Promise.resolve(jsonResponse(updated));
      if (url.endsWith("/reviews/item-1") && init?.method === "PATCH") return Promise.resolve(jsonResponse({ version: 3 }));
      throw new Error(`unexpected fetch ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={review} />);

    await user.click(screen.getByRole("button", { name: "重新查询游戏信息" }));
    const dialog = await screen.findByRole("alertdialog", { name: "对比最新查询结果" });
    const currentColumn = within(dialog).getByRole("region", { name: "当前信息" });
    const latestColumn = within(dialog).getByRole("region", { name: "最新信息" });
    expect(currentColumn).toHaveTextContent("Manual");
    expect(within(latestColumn).getByLabelText("标题")).toHaveValue("1941: Counter Attack");
    expect(within(latestColumn).getByLabelText("标题").closest("label")).toHaveClass("is-changed");
    expect(within(latestColumn).getByLabelText("简介")).toHaveValue("Long provider description");
    expect(within(latestColumn).getByLabelText("简介").closest(".metadata-compare-column-description")).not.toBeNull();
    await user.click(within(dialog).getByRole("button", { name: "应用" }));

    expect(screen.getByLabelText("标题")).toHaveValue("1941: Counter Attack");
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringMatching(/reviews\/item-1$/), expect.objectContaining({ method: "PATCH" })), { timeout: 2_000 });
    expect(await screen.findByText("已实时保存")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "保存草稿" })).not.toBeInTheDocument();
  });

  it("autosaves the first successful candidate instead of creating an unsaved draft", async () => {
    const fetchMock = vi.fn(() => Promise.resolve(jsonResponse({ version: 2 })));
    vi.stubGlobal("fetch", fetchMock);
    render(<ReviewActions review={{ ...review, candidates: [{ candidateId: "candidate-first", scrapeRunId: "run-first", providerGameId: "42", metadata: { title: "Scraped title", publisher: "Publisher" }, evidence: {}, assets: [] }] }} />);

    expect(screen.getByLabelText("标题")).toHaveValue("Scraped title");
    expect(screen.getByText(/系统会实时保存/)).toBeInTheDocument();
    await waitFor(() => expect(fetchMock).toHaveBeenCalled(), { timeout: 2_000 });
    expect(await screen.findByText("已实时保存")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "通过并发布" })).toBeEnabled();
  });

  it("keeps the cover beside metadata without rendering provider summary cards", () => {
    const { container } = render(<ReviewActions review={{ ...review, candidates: [{ candidateId: "candidate-layout", scrapeRunId: "run-layout", providerGameId: "42", metadata: { title: "Scraped title" }, evidence: {}, assets: [] }], selectedCandidateId: "candidate-layout" }} />);

    const layout = container.querySelector(".review-workflow-publish-layout");
    expect(layout).not.toBeNull();
    expect(layout?.querySelector(".review-workflow-metadata-fields")).not.toBeNull();
    expect(layout?.lastElementChild).toHaveClass("review-workflow-cover-side");
    expect(screen.getByText("当前封面")).toBeInTheDocument();
    expect(screen.queryByText("Hasheous 候选信息")).not.toBeInTheDocument();
    expect(screen.queryByText("信息来源")).not.toBeInTheDocument();
  });

  it("flushes a pending edit when the review page unmounts", async () => {
    const fetchMock = vi.fn(() => Promise.resolve(jsonResponse({ version: 2 })));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    const { unmount } = render(<ReviewActions review={review} />);

    await user.clear(screen.getByLabelText("标题"));
    await user.type(screen.getByLabelText("标题"), "Leave safely");
    unmount();

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringMatching(/reviews\/item-1$/), expect.objectContaining({ method: "PATCH", keepalive: true, body: expect.stringContaining("Leave safely") })), { timeout: 2_000 });
  });

  it("uploads a clicked cover and persists it as the manual cover selection", async () => {
    upload.uploadOne.mockResolvedValue({ uploadId: "upload-1", uploadFileId: "file-1" });
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/assets")) return Promise.resolve(jsonResponse({ assetId: "asset-1", kind: "COVER", widthPx: 600, heightPx: 900, mediaType: "image/png", url: "/api/v1/admin/review-assets/asset-1", createdAtMs: 1 }, 201));
      if (url.endsWith("/reviews/item-1") && init?.method === "PATCH") return Promise.resolve(jsonResponse({ version: 2 }));
      throw new Error(`unexpected fetch ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    const { container } = render(<ReviewActions review={review} />);
    const input = container.querySelector<HTMLInputElement>(".review-cover-panel input[type=file]");
    expect(input).not.toBeNull();
    await user.upload(input!, new File(["cover"], "cover.png", { type: "image/png" }));

    expect(await screen.findByAltText("当前选择的游戏封面")).toHaveAttribute("src", expect.stringContaining("asset-1"));
    await waitFor(() => {
      const patchCall = fetchMock.mock.calls.find(([, init]) => init?.method === "PATCH");
      expect(String(patchCall?.[1]?.body)).toContain('"coverUploadedAssetId":"asset-1"');
      expect(String(patchCall?.[1]?.body)).toContain('"coverCandidateAssetId":null');
    }, { timeout: 2_000 });
  });

  it("refreshes a stale ready validation before enabling publish", async () => {
    const fetchMock = vi.fn(() => Promise.resolve(jsonResponse({ version: 2 })));
    vi.stubGlobal("fetch", fetchMock);
    render(<ReviewActions review={{ ...review, validation: { ...review.validation!, current: false } }} />);

    expect(screen.getByRole("button", { name: "通过并发布" })).toBeDisabled();
    expect(screen.getByText("运行检查更新中")).toBeInTheDocument();
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      expect.stringMatching(/reviews\/item-1$/),
      expect.objectContaining({ method: "PATCH", body: expect.stringContaining('"title":"Manual"') }),
    ));
    await waitFor(() => expect(screen.getByRole("button", { name: "通过并发布" })).toBeEnabled());
  });

  it("lets a blocked review explicitly rerun validation after fixing dependencies", async () => {
    const fetchMock = vi.fn(() => Promise.resolve(jsonResponse({ version: 2 })));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={{ ...review, validation: { ...review.validation!, status: "BLOCKED", current: false, compatibilityCode: "LAUNCH_BIOS_MISSING" } }} />);

    expect(screen.getByRole("button", { name: "通过并发布" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "重新运行检查" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(expect.stringMatching(/reviews\/item-1$/), expect.objectContaining({ method: "PATCH" })));
    expect(router.refresh).toHaveBeenCalled();
  });

  it("flushes, uploads, validates over SSE, then enables publish from the refreshed review", async () => {
    const parentReview: ReviewWorkspace = {
      ...review,
      version: 7,
      effectiveSourceSnapshotId: "snapshot-1",
      validation: { id: "validation-1", status: "BLOCKED", current: true, compatibilityCode: "LAUNCH_PARENT_MISSING" },
      arcadeDependencies: {
        machine: "a", status: "BLOCKED", compatibilityCode: "LAUNCH_PARENT_MISSING", activeAttachment: null,
        nodes: [{ kind: "PARENT", machine: "b", requiredBy: "a", depth: 1, expectedLogicalName: "b.zip", state: "MISSING", requiredEntryCount: 1, canAttach: true, attachment: null }],
      },
    };
    const refreshed: ReviewWorkspace = {
      ...parentReview,
      version: 9,
      effectiveSourceSnapshotId: "snapshot-2",
      validation: { id: "validation-2", status: "READY", current: true, compatibilityCode: "READY" },
      arcadeDependencies: { machine: "a", status: "READY", compatibilityCode: "READY", activeAttachment: null, nodes: [{ ...parentReview.arcadeDependencies!.nodes[0], state: "SATISFIED_EXTERNAL", canAttach: false, attachment: null }] },
    };
    upload.uploadOne.mockResolvedValue({ uploadId: "upload-1", uploadFileId: "upload-file-1" });
    upload.waitForJobEvents.mockImplementation(async (_jobId: string, onProgress?: (eventType: string) => void) => { onProgress?.("parent_matched"); });
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/arcade-parent-attachments")) return Promise.resolve(new Response(JSON.stringify({ attachmentId: "attachment-1", state: "QUEUED", jobId: "job-1" }), { status: 202, headers: { "Content-Type": "application/json", ETag: '"v8"' } }));
      if (url.endsWith("/reviews/item-1") && !init?.method) return Promise.resolve(jsonResponse(refreshed));
      throw new Error(`unexpected fetch ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={parentReview} />);

    expect(screen.getByRole("button", { name: "通过并发布" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "补充 Parent ROM" }));
    const dialog = screen.getByRole("alertdialog", { name: "补充 b.zip" });
    await user.upload(within(dialog).getByLabelText("选择一个 ZIP"), new File(["parent"], "anything.zip", { type: "application/zip" }));
    await user.click(within(dialog).getByRole("button", { name: "开始上传并校验" }));

    await waitFor(() => expect(upload.waitForJobEvents).toHaveBeenCalledWith("job-1", expect.any(Function)));
    const request = fetchMock.mock.calls.find(([url]) => String(url).endsWith("/arcade-parent-attachments"));
    expect(request?.[1]?.headers).toMatchObject({ "If-Match": '"v7"' });
    expect(JSON.parse(String(request?.[1]?.body))).toEqual({ validationId: "validation-1", baseSourceSnapshotId: "snapshot-1", dependencyMachine: "b", uploadFileId: "upload-file-1" });
    await waitFor(() => expect(screen.getByRole("button", { name: "通过并发布" })).toBeEnabled());
    expect(screen.getByText("Parent ROM 已匹配，运行检查已通过")).toBeInTheDocument();
    expect(router.refresh).toHaveBeenCalled();
  });

  it("shows one short-lived notification with the server publish error", async () => {
    const fetchMock = vi.fn(() => Promise.resolve(jsonResponse({ error: { code: "INVALID_REQUEST", message: "运行检查已经过期" } }, 422)));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    const { container } = render(<ReviewActions review={review} />);

    await user.click(screen.getByRole("button", { name: "通过并发布" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("运行检查已经过期");
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(container.querySelector(".review-workflow-feedback")).toBeNull();
  });

  it("keeps scrape progress in the metadata header", async () => {
    upload.waitForJob.mockImplementation(async (_jobId: string, onProgress?: (message: string) => void) => {
      onProgress?.("RUNNING · SCRAPING");
      await new Promise(() => undefined);
    });
    const fetchMock = vi.fn(() => Promise.resolve(jsonResponse({ version: 2, state: "QUEUED", scrapeRunId: "run-1", jobId: "job-1" }, 202)));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={review} />);

    await user.click(screen.getByRole("button", { name: "重新查询游戏信息" }));
    const progress = await screen.findByRole("status", { name: "" });
    expect(progress).toHaveTextContent("正在查询游戏信息：RUNNING · SCRAPING");
    expect(progress.closest(".panel-head")).not.toBeNull();
  });

  it("publishes and discards without decision-reason fields", async () => {
    const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>((input) => {
      const url = String(input);
      if (url.endsWith("/approve")) return Promise.resolve(jsonResponse({ gameId: "game-1" }, 201));
      if (url.endsWith("/discard")) return Promise.resolve(jsonResponse({ status: "DISCARDED" }));
      throw new Error(`unexpected fetch ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    const { unmount } = render(<ReviewActions review={review} returnTo="/admin/reviews" />);

    expect(screen.queryByLabelText(/发布说明/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/丢弃原因/)).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "通过并发布" }));
    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/admin/reviews"));
    expect(fetchMock.mock.calls.find(([url]) => String(url).endsWith("/approve"))?.[1]?.body).toBe("{}");

    unmount(); router.replace.mockReset();
    render(<ReviewActions review={review} returnTo="/admin/reviews" />);
    await user.click(screen.getByRole("button", { name: "丢弃条目" }));
    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/admin/reviews"));
    expect(fetchMock.mock.calls.find(([url]) => String(url).endsWith("/discard"))?.[1]?.body).toBe("{}");
  });

  it("requires explicit confirmation before publishing an already known duplicate", async () => {
    const duplicate = { gameId: "game-existing", title: "Existing game", platformInstanceId: "platform-1", platformInstanceName: "GBA 游戏" };
    const fetchMock = vi.fn<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>(() => Promise.resolve(jsonResponse({ gameId: "game-new" }, 201)));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={{ ...review, duplicateGames: [duplicate], contentIdentityDigest: "a".repeat(64) }} />);

    expect(screen.getByText(/相同游戏文件已经关联到/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Existing game" })).toHaveAttribute("href", "/games/game-existing");
    await user.click(screen.getByRole("button", { name: "通过并发布" }));
    expect(fetchMock).not.toHaveBeenCalled();
    const dialog = await screen.findByRole("alertdialog", { name: "仍然发布为新游戏？" });
    await user.click(within(dialog).getByRole("button", { name: "仍然发布为新游戏" }));

    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/admin/reviews"));
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(JSON.parse(String(fetchMock.mock.calls[0]?.[1]?.body))).toEqual({
      duplicatePolicy: "ALLOW_NEW",
      acknowledgedGameIds: ["game-existing"],
    });
  });

  it("opens the same confirmation when a duplicate appears after the page was loaded", async () => {
    const duplicate = { gameId: "game-race", title: "Published elsewhere", platformInstanceId: "platform-1", platformInstanceName: "GBA 游戏" };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: { code: "DUPLICATE_GAME_CONFIRMATION_REQUIRED", message: "duplicate", details: { games: [duplicate] } } }, 409))
      .mockResolvedValueOnce(jsonResponse({ gameId: "game-new" }, 201));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={review} />);

    await user.click(screen.getByRole("button", { name: "通过并发布" }));
    const dialog = await screen.findByRole("alertdialog", { name: "仍然发布为新游戏？" });
    expect(within(dialog).getByRole("link", { name: "Published elsewhere" })).toHaveAttribute("href", "/games/game-race");
    await user.click(within(dialog).getByRole("button", { name: "仍然发布为新游戏" }));

    await waitFor(() => expect(router.replace).toHaveBeenCalledWith("/admin/reviews"));
    expect(JSON.parse(String(fetchMock.mock.calls[1]?.[1]?.body))).toEqual({
      duplicatePolicy: "ALLOW_NEW",
      acknowledgedGameIds: ["game-race"],
    });
  });
});
