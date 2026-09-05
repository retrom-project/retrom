import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type * as UploadModule from "@/lib/upload";
import { ReviewActions, type ReviewWorkspace } from "./review-actions";

const router = vi.hoisted(() => ({ replace: vi.fn(), refresh: vi.fn(), push: vi.fn() }));
const upload = vi.hoisted(() => ({ uploadFiles: vi.fn(), uploadOne: vi.fn(), waitForJob: vi.fn(), waitForJobEvents: vi.fn() }));

vi.mock("next/navigation", () => ({ useRouter: () => router }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "user-1" } } }) }));
vi.mock("@/lib/upload", async (importOriginal) => {
  const original = await importOriginal<typeof UploadModule>();
  return { ...original, uploadFiles: upload.uploadFiles, uploadOne: upload.uploadOne, waitForJob: upload.waitForJob, waitForJobEvents: upload.waitForJobEvents };
});

const review: ReviewWorkspace = {
  itemId: "item-1", version: 1,
  platformInstance: { id: "platform-1", name: "GBA 游戏" },
  metadata: { title: "Manual", description: "", developer: "", publisher: "", genre: "", players: null, releaseYear: null },
  validation: { id: "validation-1", status: "READY", compatibilityCode: "READY" },
  candidates: [], uploadedAssets: [], scrapeRuns: [], selectedCandidateId: null,
  selectedAssets: { coverCandidateAssetId: null, coverUploadedAssetId: null, backgroundCandidateAssetId: null, screenshotCandidateAssetIds: [] },
  defaultDosEntry: null, dosEntries: [],
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

beforeEach(() => {
  router.replace.mockReset(); router.refresh.mockReset(); router.push.mockReset();
  upload.uploadFiles.mockReset(); upload.uploadOne.mockReset(); upload.waitForJob.mockReset().mockResolvedValue(undefined); upload.waitForJobEvents.mockReset().mockResolvedValue(undefined);
  sessionStorage.clear();
});

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("ReviewActions metadata", () => {

  it("reconciles the current validation projection after autosave", async () => {
    const refreshed: ReviewWorkspace = {
      ...review,
      version: 2,
      canApprove: true,
      metadata: { ...review.metadata, title: "Current title" },
    };
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/reviews/item-1") && init?.method === "PATCH") {
        return Promise.resolve(jsonResponse({ version: 2 }));
      }
      if (url.endsWith("/reviews/item-1") && !init?.method) {
        return Promise.resolve(jsonResponse(refreshed));
      }
      throw new Error(`unexpected fetch ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={{ ...review, canApprove: false }} />);

    expect(screen.getByRole("button", { name: "通过并发布" })).toBeDisabled();
    await user.clear(screen.getByLabelText("标题"));
    await user.type(screen.getByLabelText("标题"), "Current title");

    await waitFor(() => expect(screen.getByRole("button", { name: "通过并发布" })).toBeEnabled(), {
      timeout: 2_000,
    });
    expect(fetchMock.mock.calls.map(([input]) => String(input))).toEqual([
      "/api/v1/admin/reviews/item-1",
      "/api/v1/admin/reviews/item-1",
    ]);
  });

  it("keeps an automatic scrape failure diagnosis visible on the review", () => {
    render(<ReviewActions review={{
      ...review,
      scrapeRuns: [{
        scrapeRunId: "run-older-miss", jobId: "job-older-miss", provider: "HASHEOUS",
        state: "COMPLETED", jobState: "SUCCEEDED", createdAtMs: 1, completedAtMs: 2,
        errorCode: null, evidenceCount: 1, attemptCount: 1, candidateCount: 0,
        outcomes: { hit: 0, miss: 1, rateLimited: 0, timeout: 0, invalidResponse: 0, networkError: 0 },
      }, {
        scrapeRunId: "run-rate-limited", jobId: "job-rate-limited", provider: "HASHEOUS",
        state: "COMPLETED", jobState: "SUCCEEDED", createdAtMs: 3, completedAtMs: 4,
        errorCode: null, evidenceCount: 1, attemptCount: 3, candidateCount: 0,
        outcomes: { hit: 0, miss: 0, rateLimited: 3, timeout: 0, invalidResponse: 0, networkError: 0 },
      }],
    }} />);

    expect(screen.getByText("上游限流、超时或网络异常")).toBeVisible();
  });

  it("exposes the four-step mobile review workflow without changing decision actions", () => {
    render(<ReviewActions review={review}><section>来源文件与依赖</section></ReviewActions>);
    const steps = within(screen.getByRole("navigation", { name: "审核步骤" })).getAllByRole("link");
    expect(steps.map((step) => step.textContent)).toEqual(["1来源与依赖", "2运行检查", "3发布信息", "4审核决定"]);
    expect(steps.map((step) => step.getAttribute("href"))).toEqual([
      "#review-step-source", "#review-step-runtime", "#review-step-publish", "#review-step-decision",
    ]);
    expect(screen.getByRole("button", { name: "丢弃条目" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "通过并发布" })).toBeEnabled();
  });

  it("uses the RPG validation workflow and preserves runtime pack selections in autosave", async () => {
    const rpgReview: ReviewWorkspace = {
      ...review,
      canApprove: false,
      platformInstance: { id: "rpg-directory", name: "RPG Maker MV" },
      validation: { id: "static-validation", status: "BLOCKED", compatibilityCode: "RPG_RUNTIME_VALIDATION_REQUIRED" },
      rpgMaker: {
        selectedCoreId: "rpgmaker_mv", generation: "RPGMV", evidenceGeneration: "RPGMV",
        evidenceConfidence: "MATCHED", selfContained: true, selfContainedOverride: false,
        runtimePackRequirements: [], runtimePackSelections: [], runtimeValidation: null,
      },
    };
    const refreshedRpgReview = { ...rpgReview, version: 2 };
    const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => Promise.resolve(
      init?.method === "PATCH" ? jsonResponse({ version: 2 }) : jsonResponse(refreshedRpgReview),
    ));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={rpgReview} />);

    expect(screen.getByRole("heading", { name: "RPG Maker 运行验证" })).toBeInTheDocument();
    expect(screen.getByText("RPG Maker MV")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "运行游戏" })).toBeEnabled();
    expect(screen.queryByText("第 5 秒运行截图")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "通过并发布" })).toBeDisabled();

    await user.clear(screen.getByLabelText("标题"));
    await user.type(screen.getByLabelText("标题"), "MV Project");
    await waitFor(() => expect(fetchMock).toHaveBeenCalled(), { timeout: 2_000 });
    const request = fetchMock.mock.calls.find(([, init]) => init?.method === "PATCH")?.[1] as RequestInit;
    expect(JSON.parse(String(request.body))).toMatchObject({
      metadata: { title: "MV Project" }, runtimePackSelections: [], rpgSelfContainedOverride: false,
    });
  });

});

describe("ReviewActions metadata continuation", () => {
  it("opens one comparison dialog and autosaves the applied result", async () => {
    const candidate = { candidateId: "candidate-1", scrapeRunId: "run-1", providerGameId: "50192", metadata: { title: "1941: Counter Attack", description: "Long provider description", publisher: "Capcom" }, evidence: {}, assets: [{ candidateAssetId: "cover-1", kind: "COVER" as const, ordinal: 0, status: "READY", widthPx: 320, heightPx: 480, mediaType: "image/png", errorCode: null }] };
    const updated: ReviewWorkspace = { ...review, version: 3, candidates: [candidate], scrapeRuns: [{ scrapeRunId: "run-1", jobId: "job-1", provider: "HASHEOUS", state: "COMPLETED", jobState: "SUCCEEDED", createdAtMs: 1, completedAtMs: 2, errorCode: null, evidenceCount: 1, attemptCount: 1, candidateCount: 1, outcomes: { hit: 1, miss: 0, rateLimited: 0, timeout: 0, invalidResponse: 0, networkError: 0 } }] };
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/scrape-candidates")) {return Promise.resolve(jsonResponse({ version: 2, state: "QUEUED", scrapeRunId: "run-1", jobId: "job-1" }, 202));}
      if (url.endsWith("/reviews/item-1") && !init?.method) {return Promise.resolve(jsonResponse(updated));}
      if (url.endsWith("/reviews/item-1") && init?.method === "PATCH") {return Promise.resolve(jsonResponse({ version: 3 }));}
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
    const candidate = { candidateId: "candidate-first", scrapeRunId: "run-first", providerGameId: "42", metadata: { title: "Scraped title", publisher: "Publisher" }, evidence: {}, assets: [] };
    const candidateReview: ReviewWorkspace = { ...review, candidates: [candidate] };
    const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => Promise.resolve(
      init?.method === "PATCH" ? jsonResponse({ version: 2 }) : jsonResponse({ ...candidateReview, version: 2 }),
    ));
    vi.stubGlobal("fetch", fetchMock);
    render(<ReviewActions review={candidateReview} />);

    expect(screen.getByLabelText("标题")).toHaveValue("Scraped title");
    expect(screen.getByText(/系统会实时保存/)).toBeInTheDocument();
    await waitFor(() => expect(fetchMock).toHaveBeenCalled(), { timeout: 2_000 });
    expect(await screen.findByText("已实时保存")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "通过并发布" })).toBeEnabled();
  });

  it("autosaves selected existing tags with the complete review draft", async () => {
    const actionTag = { tagId: "tag-action", name: "动作" };
    const coopTag = { tagId: "tag-coop", name: "双人合作" };
    const taggedReview: ReviewWorkspace = { ...review, tags: [actionTag] };
    const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => Promise.resolve(
      init?.method === "PATCH" ? jsonResponse({ version: 2 }) : jsonResponse({ ...taggedReview, version: 2, tags: [actionTag, coopTag] }),
    ));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={taggedReview} activeTags={[actionTag, coopTag]} />);

    await user.type(screen.getByRole("combobox", { name: "游戏标签" }), "合作");
    await user.keyboard("{Enter}");
    await waitFor(() => {
      const patch = fetchMock.mock.calls.find(([, init]) => init?.method === "PATCH");
      expect(JSON.parse(String(patch?.[1]?.body)).tagIds).toEqual(["tag-action", "tag-coop"]);
    }, { timeout: 2_000 });
    expect(screen.getByText("已实时保存")).toBeVisible();
  });

  it("keeps the cover beside metadata without rendering provider summary cards", () => {
    const { container } = render(<ReviewActions review={{ ...review, candidates: [{ candidateId: "candidate-layout", scrapeRunId: "run-layout", providerGameId: "42", metadata: { title: "Scraped title" }, evidence: {}, assets: [] }], selectedCandidateId: "candidate-layout" }} />);

    const layout = container.querySelector(".review-workflow-publish-layout");
    const fields = layout?.querySelector(".review-workflow-metadata-fields");
    const tagEditor = fields?.querySelector(".review-tag-editor");
    expect(layout).not.toBeNull();
    expect(fields).not.toBeNull();
    expect(tagEditor).not.toBeNull();
    expect(fields?.lastElementChild).toBe(tagEditor);
    expect(layout?.lastElementChild).toHaveClass("review-workflow-cover-side");
    expect(screen.getByText("当前封面")).toBeInTheDocument();
    expect(screen.queryByText("Hasheous 候选信息")).not.toBeInTheDocument();
    expect(screen.queryByText("信息来源")).not.toBeInTheDocument();
  });

  it("uses Pegasus cover and a manual centered video preview as review source media", () => {
    const { container } = render(<ReviewActions review={{
      ...review,
      sourceMedia: {
        sourceKind: "PEGASUS",
        sourceRefId: "pegasus-item-1",
        pegasusImportId: "pegasus-import-1",
        sourceLabel: "FC",
        coverUrl: "/api/v1/admin/review-assets/pegasus-item-1?kind=COVER",
        coverWidthPx: 320,
        coverHeightPx: 480,
        videoUrl: "/api/v1/admin/review-assets/pegasus-item-1?kind=VIDEO",
      },
    }} />);

    expect(screen.getByText("来源：Pegasus · FC")).toBeVisible();
    expect(screen.getByText("已读取 Pegasus 信息")).toBeVisible();
    expect(screen.getByAltText("当前选择的游戏封面")).toHaveAttribute("src", expect.stringContaining("kind=COVER"));
    const video = container.querySelector<HTMLVideoElement>(".review-source-video video");
    expect(video).toHaveAttribute("src", "/api/v1/admin/review-assets/pegasus-item-1?kind=VIDEO");
    expect(video).toHaveAttribute("controls");
    expect(video?.autoplay).toBe(false);
  });

  it("labels EmulationStation source media without presenting it as scraped metadata", () => {
    const { container } = render(<ReviewActions review={{
      ...review,
      sourceMedia: {
        sourceKind: "EMULATIONSTATION",
        sourceRefId: "emulationstation-item-1",
        emulationStationImportId: "emulationstation-import-1",
        sourceLabel: "NES gamelist.xml",
        coverUrl: "/api/v1/admin/review-assets/emulationstation-item-1?kind=COVER",
        coverWidthPx: 320,
        coverHeightPx: 480,
        videoUrl: "/api/v1/admin/review-assets/emulationstation-item-1?kind=VIDEO",
      },
    }} />);

    expect(screen.getByText("来源：EmulationStation · NES gamelist.xml")).toBeVisible();
    expect(screen.getByText("已读取 Gamelist 信息")).toBeVisible();
    expect(screen.getByText("EmulationStation 视频预览")).toBeVisible();
    expect(container.querySelector<HTMLVideoElement>(".review-source-video video")).toHaveAttribute(
      "src",
      "/api/v1/admin/review-assets/emulationstation-item-1?kind=VIDEO",
    );
  });

  it("flushes a pending edit when the review page unmounts", async () => {
    const fetchMock = vi.fn((_input: RequestInfo | URL, init?: RequestInit) => Promise.resolve(
      init?.method === "PATCH" ? jsonResponse({ version: 2 }) : jsonResponse({ ...review, version: 2 }),
    ));
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
      if (url.endsWith("/assets")) {return Promise.resolve(jsonResponse({ assetId: "asset-1", kind: "COVER", widthPx: 600, heightPx: 900, mediaType: "image/png", url: "/api/v1/admin/review-assets/asset-1", createdAtMs: 1 }, 201));}
      if (url.endsWith("/reviews/item-1") && init?.method === "PATCH") {return Promise.resolve(jsonResponse({ version: 2 }));}
      if (url.endsWith("/reviews/item-1") && !init?.method) {return Promise.resolve(jsonResponse({ ...review, version: 2, uploadedAssets: [{ assetId: "asset-1", kind: "COVER", widthPx: 600, heightPx: 900, mediaType: "image/png", url: "/api/v1/admin/review-assets/asset-1", createdAtMs: 1 }] }));}
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

  it("does not expose runtime deployment contract or recheck state", () => {
    render(<ReviewActions review={review} />);

    expect(screen.queryByText(/Runtime Target/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Runtime 待重检/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "重新运行检查" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "运行游戏" })).toBeEnabled();
  });

  it("opens a best-effort game preview without requiring publish-ready validation", async () => {
    const replace = vi.fn();
    const popup = { closed: false, close: vi.fn(), location: { replace }, document: { title: "", body: { style: { cssText: "" }, textContent: "" } } } as unknown as Window;
    vi.spyOn(window, "open").mockReturnValue(popup);
    const blocked = {
      ...review,
      metadata: { ...review.metadata, description: "界".repeat(12_167) },
      canApprove: false,
      validation: { ...review.validation!, status: "BLOCKED", compatibilityCode: "LAUNCH_PARENT_MISSING" },
    };
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/previews")) {return Promise.resolve(jsonResponse({ previewId: "preview-best-effort", playUrl: "/admin/review-previews/preview-best-effort", captureAllowed: true, captureAfterMs: 5000 }, 201));}
      if (init?.method === "PATCH") {return Promise.resolve(jsonResponse({ error: { code: "INVALID_REQUEST" } }, 400));}
      throw new Error(`unexpected fetch ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={blocked} />);

    expect(screen.getByRole("button", { name: "运行游戏" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "运行游戏" }));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/admin/review-previews/preview-best-effort"));
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/reviews/item-1/previews", expect.objectContaining({ method: "POST" }));
    expect(fetchMock.mock.calls.some(([, init]) => init?.method === "PATCH")).toBe(false);
    expect(screen.getByText("等待运行截图")).toBeVisible();
  });

  it("shows the current five-second runtime screenshot", () => {
    render(<ReviewActions review={{ ...review, runtimeScreenshot: {
      screenshotId: "shot-1", validationId: "validation-1", providerId: "emulatorjs", targetId: "mgba",
      widthPx: 640, heightPx: 480, capturedAfterMs: 5000, capturedAtMs: 123,
      url: "/api/v1/admin/review-assets/shot-1",
    } }} />);

    expect(screen.getByAltText("Manual 的第 5 秒运行截图")).toHaveAttribute("src", expect.stringContaining("shot-1"));
    expect(screen.getByRole("button", { name: "运行游戏" })).toBeVisible();
  });
});

describe("ReviewActions validation", () => {

  it("enables an administrator screenshot override for a blocked validation", () => {
    render(<ReviewActions review={{
      ...review,
      canApprove: true,
      validation: { ...review.validation!, status: "BLOCKED", compatibilityCode: "LAUNCH_PARENT_MISSING" },
      runtimeScreenshot: {
        screenshotId: "shot-blocked", validationId: "validation-1", providerId: "emulatorjs", targetId: "mgba",
        widthPx: 640, heightPx: 480, capturedAfterMs: 5000, capturedAtMs: 123,
        url: "/api/v1/admin/review-assets/shot-blocked",
      },
    }} />);

    expect(screen.getByText("已取得运行截图")).toBeVisible();
    expect(screen.getByText("已取得第 5 秒运行截图，可由管理员确认后发布。")).toBeVisible();
    expect(screen.getByRole("button", { name: "通过并发布" })).toBeEnabled();
  });

  it("flushes, uploads, validates over SSE, then enables publish from the refreshed review", async () => {
    const parentReview: ReviewWorkspace = {
      ...review,
      version: 7,
      effectiveSourceSnapshotId: "snapshot-1",
      validation: { id: "validation-1", status: "BLOCKED", compatibilityCode: "LAUNCH_PARENT_MISSING" },
      arcadeDependencies: {
        machine: "a", status: "BLOCKED", compatibilityCode: "LAUNCH_PARENT_MISSING", activeAttachment: null,
        nodes: [{ kind: "PARENT", machine: "b", requiredBy: "a", depth: 1, expectedLogicalName: "b.zip", state: "MISSING", requiredEntryCount: 1, canAttach: true, attachment: null }],
      },
    };
    const refreshed: ReviewWorkspace = {
      ...parentReview,
      version: 9,
      effectiveSourceSnapshotId: "snapshot-2",
      validation: { id: "validation-2", status: "READY", compatibilityCode: "READY" },
      arcadeDependencies: { machine: "a", status: "READY", compatibilityCode: "READY", activeAttachment: null, nodes: [{ ...parentReview.arcadeDependencies!.nodes[0], state: "SATISFIED_EXTERNAL", canAttach: false, attachment: null }] },
    };
    upload.uploadOne.mockResolvedValue({ uploadId: "upload-1", uploadFileId: "upload-file-1" });
    upload.waitForJobEvents.mockImplementation(async (_jobId: string, onProgress?: (eventType: string) => void) => { onProgress?.("parent_matched"); });
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/arcade-parent-attachments")) {return Promise.resolve(new Response(JSON.stringify({ attachmentId: "attachment-1", state: "QUEUED", jobId: "job-1" }), { status: 202, headers: { "Content-Type": "application/json", ETag: '"v8"' } }));}
      if (url.endsWith("/reviews/item-1") && !init?.method) {return Promise.resolve(jsonResponse(refreshed));}
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

  it("shows ordered multi-disc evidence and uploads exactly the missing CHD set", async () => {
    const blocked: ReviewWorkspace = {
      ...review,
      version: 4,
      validation: { id: "validation-multi-1", status: "BLOCKED", compatibilityCode: "LAUNCH_MULTI_DISC_INCOMPLETE" },
      multiDisc: {
        contentKind: "MULTI_DISC",
        playlist: { name: "game.m3u", sizeBytes: 18, sha256: "a".repeat(64) },
        discCount: 2, presentDiscCount: 1, missingDiscCount: 1, totalPresentBytes: 4,
        maxDiscs: 8, maxTotalBytes: 1024,
        entries: [
          { index: 0, discIndex: 0, label: "光盘 1", sourceReference: "one.chd", canonicalName: "disc-001.chd", state: "PRESENT", logicalName: "one.chd", sizeBytes: 4, sha256: "b".repeat(64) },
          { index: 1, discIndex: 1, label: "光盘 2", sourceReference: "two.chd", canonicalName: "disc-002.chd", state: "MISSING", logicalName: null, sizeBytes: null, sha256: null },
        ],
        missingReferences: ["two.chd"], canAttachMissingDiscs: true, latestAttachment: null, activeAttachment: null,
      },
    };
    const refreshed: ReviewWorkspace = {
      ...blocked,
      version: 5,
      validation: { id: "validation-multi-2", status: "READY", compatibilityCode: "READY" },
      multiDisc: {
        ...blocked.multiDisc!, presentDiscCount: 2, missingDiscCount: 0, missingReferences: [], canAttachMissingDiscs: false,
        entries: blocked.multiDisc!.entries.map((entry) => entry.discIndex === 1 ? { ...entry, state: "PRESENT", logicalName: "two.chd", sizeBytes: 4, sha256: "c".repeat(64) } : entry),
      },
    };
    upload.uploadFiles.mockResolvedValue({ uploadId: "upload-multi", uploadFileIds: ["file-two"] });
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/multi-disc-attachments")) {return Promise.resolve(new Response(JSON.stringify({ jobId: "job-multi", reviewVersion: 5 }), { status: 202, headers: { "Content-Type": "application/json", ETag: '"v5"' } }));}
      if (url.endsWith("/reviews/item-1") && !init?.method) {return Promise.resolve(jsonResponse(refreshed));}
      throw new Error(`unexpected fetch ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={blocked} />);

    expect(screen.getByText(/game\.m3u · 18 B · SHA-256/)).toBeVisible();
    expect(screen.getByText("光盘 1 · one.chd")).toBeVisible();
    expect(screen.getByText("光盘 2 · two.chd")).toBeVisible();
    const trigger = screen.getByRole("button", { name: "上传全部缺失光盘" });
    await user.click(trigger);
    const drawer = screen.getByRole("dialog", { name: "上传全部缺失光盘" });
    expect(within(drawer).getByRole("heading", { name: "上传全部缺失光盘" })).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(trigger).toHaveFocus();
    await user.click(trigger);
    await user.upload(within(screen.getByRole("dialog", { name: "上传全部缺失光盘" })).getByLabelText("选择当前全部缺失 CHD"), new File(["disc"], "two.chd"));
    await user.click(within(screen.getByRole("dialog", { name: "上传全部缺失光盘" })).getByRole("button", { name: "上传并校验" }));

    await waitFor(() => expect(upload.uploadFiles).toHaveBeenCalledTimes(1));
    const request = fetchMock.mock.calls.find(([url]) => String(url).endsWith("/multi-disc-attachments"));
    expect(request?.[1]?.headers).toMatchObject({ "If-Match": '"v4"' });
    expect(request?.[1]?.body).toBe(JSON.stringify({ uploadId: "upload-multi" }));
    expect(await screen.findByText("盘序完整")).toBeVisible();
    expect(screen.getByText("缺失光盘已补齐，正在更新审核结果")).toBeVisible();
    expect(screen.getByRole("button", { name: "通过并发布" })).toBeEnabled();
  });

  it("keeps the exact-set drawer selection after a review version conflict", async () => {
    const blocked: ReviewWorkspace = {
      ...review, version: 4, canApprove: false,
      validation: { id: "validation-multi-1", status: "BLOCKED", compatibilityCode: "LAUNCH_MULTI_DISC_INCOMPLETE" },
      multiDisc: {
        contentKind: "MULTI_DISC", playlist: { name: "game.m3u", sizeBytes: 18, sha256: "a".repeat(64) },
        discCount: 2, presentDiscCount: 1, missingDiscCount: 1, totalPresentBytes: 4, maxDiscs: 8, maxTotalBytes: 1024,
        entries: [
          { index: 0, discIndex: 0, label: "光盘 1", sourceReference: "one.chd", canonicalName: "disc-001.chd", state: "PRESENT", logicalName: "one.chd", sizeBytes: 4, sha256: "b".repeat(64) },
          { index: 1, discIndex: 1, label: "光盘 2", sourceReference: "two.chd", canonicalName: "disc-002.chd", state: "MISSING", logicalName: null, sizeBytes: null, sha256: null },
        ],
        missingReferences: ["two.chd"], canAttachMissingDiscs: true, latestAttachment: null, activeAttachment: null,
      },
    };
    upload.uploadFiles.mockResolvedValue({ uploadId: "upload-multi", uploadFileIds: ["file-two"] });
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/multi-disc-attachments")) {return Promise.resolve(jsonResponse({ error: { code: "REVIEW_VERSION_CONFLICT", message: "审核条目已发生变化" } }, 409));}
      if (url.endsWith("/reviews/item-1") && !init?.method) {return Promise.resolve(jsonResponse({ ...blocked, version: 5 }));}
      throw new Error(`unexpected fetch ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={blocked} />);

    await user.click(screen.getByRole("button", { name: "上传全部缺失光盘" }));
    const drawer = screen.getByRole("dialog", { name: "上传全部缺失光盘" });
    await user.upload(within(drawer).getByLabelText("选择当前全部缺失 CHD"), new File(["disc"], "two.chd"));
    await user.click(within(drawer).getByRole("button", { name: "上传并校验" }));

    expect(await screen.findByText("审核条目已发生变化")).toBeVisible();
    expect(screen.getByRole("dialog", { name: "上传全部缺失光盘" })).toBeVisible();
    expect(within(screen.getByRole("dialog", { name: "上传全部缺失光盘" })).getByText("已选择")).toBeVisible();
  });

  it("retries a retryable multi-disc validation without uploading the files again", async () => {
    const failedAttachment = { attachmentId: "attachment-retry", state: "FAILED_RETRYABLE", errorCode: "REVIEW_MULTI_DISC_VALIDATION_UNAVAILABLE", jobId: "job-retry", jobState: "FAILED", version: 2, jobVersion: 3, canRetry: true };
    const blocked: ReviewWorkspace = {
      ...review, version: 6, canApprove: false,
      validation: { id: "validation-multi-1", status: "BLOCKED", compatibilityCode: "LAUNCH_MULTI_DISC_INCOMPLETE" },
      multiDisc: {
        contentKind: "MULTI_DISC", playlist: { name: "game.m3u", sizeBytes: 18, sha256: "a".repeat(64) },
        discCount: 2, presentDiscCount: 1, missingDiscCount: 1, totalPresentBytes: 4, maxDiscs: 8, maxTotalBytes: 1024,
        entries: [
          { index: 0, discIndex: 0, label: "光盘 1", sourceReference: "one.chd", canonicalName: "disc-001.chd", state: "PRESENT", logicalName: "one.chd", sizeBytes: 4, sha256: "b".repeat(64) },
          { index: 1, discIndex: 1, label: "光盘 2", sourceReference: "two.chd", canonicalName: "disc-002.chd", state: "MISSING", logicalName: null, sizeBytes: null, sha256: null },
        ],
        missingReferences: ["two.chd"], canAttachMissingDiscs: false, latestAttachment: failedAttachment, activeAttachment: null,
      },
    };
    const refreshed: ReviewWorkspace = {
      ...blocked, version: 7, canApprove: true,
      validation: { id: "validation-multi-2", status: "READY", compatibilityCode: "READY" },
      multiDisc: {
        ...blocked.multiDisc!, presentDiscCount: 2, missingDiscCount: 0, missingReferences: [], latestAttachment: { ...failedAttachment, state: "ACCEPTED", errorCode: null, jobState: "SUCCEEDED", canRetry: false },
        entries: blocked.multiDisc!.entries.map((entry) => entry.discIndex === 1 ? { ...entry, state: "PRESENT", logicalName: "two.chd", sizeBytes: 4, sha256: "c".repeat(64) } : entry),
      },
    };
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/jobs/job-retry") && !init?.method) {return Promise.resolve(jsonResponse({ version: 3 }));}
      if (url.endsWith("/jobs/job-retry/retry")) {return Promise.resolve(jsonResponse({ state: "QUEUED" }, 202));}
      if (url.endsWith("/reviews/item-1") && !init?.method) {return Promise.resolve(jsonResponse(refreshed));}
      throw new Error(`unexpected fetch ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={blocked} />);

    await user.click(screen.getByRole("button", { name: "重试校验" }));
    await waitFor(() => expect(upload.waitForJob).toHaveBeenCalledWith("job-retry", expect.any(Function)));
    const retryRequest = fetchMock.mock.calls.find(([url]) => String(url).endsWith("/jobs/job-retry/retry"));
    expect(retryRequest?.[1]?.headers).toMatchObject({ "If-Match": '"v3"' });
    expect(upload.uploadFiles).not.toHaveBeenCalled();
    expect(await screen.findByText("盘序完整")).toBeVisible();
    expect(screen.getByRole("button", { name: "通过并发布" })).toBeEnabled();
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
});

describe("ReviewActions decisions", () => {

  it("treats an optimistic review conflict as terminal without hidden retries", async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/approve")) {
        return Promise.resolve(jsonResponse({ error: { code: "REVIEW_VERSION_CONFLICT", message: "审核条目已在其他位置发生变化" } }, 409));
      }
      throw new Error(`unexpected fetch ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<ReviewActions review={review} />);

    await user.click(screen.getByRole("button", { name: "通过并发布" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("审核条目已在其他位置发生变化");
    expect(fetchMock.mock.calls.filter(([url]) => String(url).endsWith("/approve"))).toHaveLength(1);
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/reviews/item-1"))).toBe(false);
    expect(router.replace).not.toHaveBeenCalled();
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
      if (url.endsWith("/approve")) {return Promise.resolve(jsonResponse({ gameId: "game-1" }, 201));}
      if (url.endsWith("/discard")) {return Promise.resolve(jsonResponse({ status: "DISCARDED" }));}
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
