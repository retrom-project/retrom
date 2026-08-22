import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PegasusImportDetailManager, PegasusImportDrawer, type PegasusImportSummary, type PegasusItem, type PegasusPlatformInstance } from "./pegasus-import-manager";

const router = vi.hoisted(() => ({ push: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));

const root = { id: "games", label: "游戏资料库", status: "AVAILABLE" as const };
const platform: PegasusPlatformInstance = { id: "11111111-1111-4111-8111-111111111111", name: "NES 游戏", platformName: "Nintendo Entertainment System", defaultCoreId: "fceumm", defaultCoreName: "FCEUmm", enabled: true };
const activeTag = { tagId: "77777777-7777-4777-8777-777777777770", name: "双人" };

function summary(state: PegasusImportSummary["state"], version: number, overrides: Partial<PegasusImportSummary> = {}): PegasusImportSummary {
  return {
    id: "22222222-2222-4222-8222-222222222222", root: { id: root.id, label: root.label }, sourceRelativePath: "Roms/FC", state,
    phase: state === "SCANNING" ? "DISCOVERING_METADATA" : null,
    scanJobId: "33333333-3333-4333-8333-333333333333", importJobId: state === "QUEUED" ? "44444444-4444-4444-8444-444444444444" : null,
    counts: { metadata: 1, invalidMetadata: 0, collections: 1, games: 3, estimatedSourceBytes: 1024, mappedCollections: state === "QUEUED" ? 1 : 0, skippedCollections: 0, processable: 2, blocked: 1, reviewPending: 0, published: 0, reviewDiscarded: 0, existing: 0, failed: 0, cancelled: 0, mediaWarnings: 1, covers: 3, videos: 2 },
    mappingVersion: version, version, createdBy: { id: "55555555-5555-4555-8555-555555555555", displayName: "Admin" }, lastErrorCode: null, retryable: false,
    createdAtMs: 1, updatedAtMs: 2, expiresAtMs: 9999999999999, completedAtMs: null, ...overrides,
  };
}

function json(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  cleanup();
  router.push.mockReset();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("PegasusImportDrawer", () => {
  it("scans, requires an explicit collection mapping, then starts the frozen plan", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const scanning = summary("SCANNING", 1);
    const awaiting = summary("AWAITING_MAPPING", 2);
    const mapped = summary("AWAITING_MAPPING", 3, { counts: { ...awaiting.counts, mappedCollections: 1 } });
    const queued = summary("QUEUED", 4, { importJobId: "44444444-4444-4444-8444-444444444444", counts: mapped.counts });
    const collection = { id: "66666666-6666-4666-8666-666666666666", metadataRelativePath: "metadata.pegasus.txt", segmentOrdinal: 0, name: "FC", shortName: "nes", description: "", gameCount: 3, issueCount: 1, mappingAction: null, targetPlatformInstanceId: null, targetPlatformInstanceName: null, targetDefaultCoreId: null, targetDefaultCoreName: null, tagSnapshot: [], ignoredRules: [], warningFields: [] };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json({ rootId: root.id, path: "", items: [{ name: "Roms", relativePath: "Roms" }], nextCursor: null }))
      .mockResolvedValueOnce(json(scanning, 202))
      .mockResolvedValueOnce(json(awaiting))
      .mockResolvedValueOnce(json({ items: [collection], nextCursor: null }))
      .mockResolvedValueOnce(json(mapped))
      .mockResolvedValueOnce(json(queued, 202));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<PegasusImportDrawer open roots={[root]} platformInstances={[platform]} activeTags={[activeTag]} onClose={vi.fn()} onStarted={vi.fn()} />);

    await screen.findByRole("button", { name: /Roms/ });
    await user.click(screen.getByRole("button", { name: "扫描此目录" }));
    expect(await screen.findByText("发现 metadata")).toBeVisible();
    await act(async () => { vi.advanceTimersByTime(2_000); });
    expect(await screen.findByRole("combobox", { name: "FC 处理方式" })).toHaveValue("");
    expect(screen.getByRole("button", { name: "确认映射" })).toBeDisabled();
    await user.type(screen.getByRole("combobox", { name: "批次标签" }), "双");
    await user.keyboard("{Enter}");
    await user.click(screen.getByRole("button", { name: "应用到所有未跳过 Collection" }));
    expect(screen.getByRole("status")).toHaveTextContent("1 个未跳过 Collection，覆盖 3 个游戏");
    await user.selectOptions(screen.getByRole("combobox", { name: "FC 处理方式" }), `IMPORT:${platform.id}`);
    expect(screen.getAllByRole("button", { name: `移除标签“${activeTag.name}”` })).toHaveLength(2);
    await user.click(screen.getByRole("button", { name: "确认映射" }));
    expect(await screen.findByText("1 个处理 · 0 个跳过")).toBeVisible();
    expect(screen.getByText("1 个 Collection · 3 个游戏")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "开始准备审核事项" }));

    await waitFor(() => expect(router.push).toHaveBeenCalledWith(`/admin/imports/server/pegasus/${queued.id}`));
    const mappingRequest = fetchMock.mock.calls[4]?.[0] as Request;
    expect(await mappingRequest.clone().json()).toEqual({ mappings: [{ collectionId: collection.id, action: "IMPORT", platformInstanceId: platform.id, tagIds: [activeTag.tagId] }] });
    const startRequest = fetchMock.mock.calls[5]?.[0] as Request;
    expect(await startRequest.clone().json()).toEqual({ version: 3 });
  });

  it("returns a fully saved mapping directly to the import confirmation step", async () => {
    const mapped = summary("AWAITING_MAPPING", 3, { counts: { ...summary("AWAITING_MAPPING", 2).counts, mappedCollections: 1 } });
    const collection = { id: "66666666-6666-4666-8666-666666666666", metadataRelativePath: "metadata.pegasus.txt", segmentOrdinal: 0, name: "FC", shortName: "nes", description: "", gameCount: 3, issueCount: 1, mappingAction: "IMPORT" as const, targetPlatformInstanceId: platform.id, targetPlatformInstanceName: platform.name, targetDefaultCoreId: platform.defaultCoreId, targetDefaultCoreName: platform.defaultCoreName, tagSnapshot: [], ignoredRules: [], warningFields: [] };
    vi.stubGlobal("fetch", vi.fn()
      .mockResolvedValueOnce(json(mapped))
      .mockResolvedValueOnce(json({ items: [collection], nextCursor: null })));

    render(<PegasusImportDrawer open roots={[root]} platformInstances={[platform]} resumablePlan={mapped} onClose={vi.fn()} onStarted={vi.fn()} />);

    expect(await screen.findByText("1 个处理 · 0 个跳过")).toBeVisible();
    expect(screen.getByRole("button", { name: "开始准备审核事项" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "确认映射" })).not.toBeInTheDocument();
  });

  it("keeps focus, scroll lock, and mapping drafts stable when the parent refreshes the same plan", async () => {
    const awaiting = summary("AWAITING_MAPPING", 2);
    const collection = { id: "66666666-6666-4666-8666-666666666666", metadataRelativePath: "metadata.pegasus.txt", segmentOrdinal: 0, name: "FC", shortName: "nes", description: "", gameCount: 3, issueCount: 1, mappingAction: null, targetPlatformInstanceId: null, targetPlatformInstanceName: null, targetDefaultCoreId: null, targetDefaultCoreName: null, tagSnapshot: [], ignoredRules: [], warningFields: [] };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json(awaiting))
      .mockResolvedValueOnce(json({ items: [collection], nextCursor: null }));
    vi.stubGlobal("fetch", fetchMock);
    document.body.style.overflow = "auto";
    const user = userEvent.setup();
    const onClose = vi.fn();
    const onStarted = vi.fn();
    const view = render(<StrictMode><PegasusImportDrawer open roots={[root]} platformInstances={[platform]} resumablePlan={awaiting} onClose={onClose} onStarted={onStarted} /></StrictMode>);

    const mapping = await screen.findByRole("combobox", { name: "FC 处理方式" });
    expect(document.body.style.overflow).toBe("hidden");
    await user.selectOptions(mapping, `IMPORT:${platform.id}`);
    mapping.focus();
    view.rerender(<StrictMode><PegasusImportDrawer open roots={[root]} platformInstances={[platform]} resumablePlan={{ ...awaiting, version: 3, updatedAtMs: 3 }} onClose={onClose} onStarted={onStarted} /></StrictMode>);

    await act(async () => { await Promise.resolve(); });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(mapping).toHaveValue(`IMPORT:${platform.id}`);
    expect(mapping).toHaveFocus();

    view.rerender(<StrictMode><PegasusImportDrawer open={false} roots={[root]} platformInstances={[platform]} resumablePlan={{ ...awaiting, version: 3, updatedAtMs: 3 }} onClose={onClose} onStarted={onStarted} /></StrictMode>);
    expect(document.body.style.overflow).toBe("auto");
  });
});

describe("PegasusImportDetailManager", () => {
  it("reopens the exact awaiting plan for mapping without rescanning the directory", async () => {
    const awaiting = summary("AWAITING_MAPPING", 2);
    const collection = { id: "66666666-6666-4666-8666-666666666666", metadataRelativePath: "metadata.pegasus.txt", segmentOrdinal: 0, name: "FC", shortName: "nes", description: "", gameCount: 3, issueCount: 1, mappingAction: null, targetPlatformInstanceId: null, targetPlatformInstanceName: null, targetDefaultCoreId: null, targetDefaultCoreName: null, tagSnapshot: [], ignoredRules: [], warningFields: [] };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(json(awaiting))
      .mockResolvedValueOnce(json({ items: [collection], nextCursor: null }));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();

    render(<PegasusImportDetailManager initialSummary={awaiting} initialItems={{ items: [], nextCursor: null }} collections={[collection]} roots={[root]} platformInstances={[platform]} initialFilters={{ query: "", outcome: "", warning: "", collectionId: "" }} />);

    expect(screen.queryByRole("link", { name: "新建 Pegasus 导入" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "继续映射" }));
    const mapping = await screen.findByRole("combobox", { name: "FC 处理方式" });
    expect(screen.queryByRole("button", { name: "扫描此目录" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "确认映射" })).toBeDisabled();
    await user.selectOptions(mapping, `IMPORT:${platform.id}`);
    expect(screen.getByRole("button", { name: "确认映射" })).toBeEnabled();
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect((fetchMock.mock.calls[0]?.[0] as Request).url).toContain(`/api/v1/admin/pegasus-imports/${awaiting.id}`);
  });

  it("shows the exact runtime blocker, evidence, and remediation to administrators", async () => {
    const blocked: PegasusItem = {
      id: "77777777-7777-4777-8777-777777777777", title: "1944 循环的征服者",
      collectionId: "66666666-6666-4666-8666-666666666666", collectionName: "飞机街机",
      targetPlatformInstanceId: platform.id, targetPlatformInstanceName: "FBNeo 游戏",
      metadataRelativePath: "metadata.pegasus.txt", executionState: "REVIEW_PENDING", contentKind: "SINGLE_FILE",
      tags: [],
      media: { cover: "READY", video: "MISSING" }, warnings: [], discoveryCode: null,
      errorCode: null, retryable: false, publishedGameId: null, existingGameId: null,
      reviewItemId: "99999999-9999-4999-8999-999999999999",
      failureDetails: null, existingMatches: [], updatedAtMs: 2,
      runtimeCheck: {
        status: "BLOCKED", code: "LAUNCH_PARENT_MISSING", coreId: "fbneo", coreName: "FinalBurn Neo",
        machine: "1944j", missingEntries: ["1944.zip"], mismatchedEntries: [], bios: [], missingDiscs: [],
        dependencies: [{ kind: "PARENT", machine: "1944", requiredBy: "1944j", expectedLogicalName: "1944.zip", state: "MISSING", requiredEntries: ["nffe.03"] }],
      },
    };
    const result = summary("COMPLETED", 4, { importJobId: "44444444-4444-4444-8444-444444444444", retryable: false, completedAtMs: 3 });
    const user = userEvent.setup();

    render(<PegasusImportDetailManager initialSummary={result} initialItems={{ items: [blocked], nextCursor: null }} collections={[]} roots={[root]} platformInstances={[platform]} initialFilters={{ query: "", outcome: "", warning: "", collectionId: "" }} />);

    expect(screen.queryByRole("button", { name: "重试失败条目" })).not.toBeInTheDocument();
    expect(screen.getAllByText("缺少父 ROM")[0]).toBeVisible();
    await user.click(screen.getByText("查看具体原因与处理建议"));
    expect(screen.getByText("LAUNCH_PARENT_MISSING")).toBeVisible();
    expect(screen.getAllByText("1944.zip")[0]).toBeVisible();
    expect(screen.getByText(/把缺失的父 ROM ZIP 放入同一 Pegasus 来源/)).toBeVisible();
  });

  it("shows structured internal failure context instead of only the aggregate error code", async () => {
    const failed: PegasusItem = {
      id: "88888888-8888-4888-8888-888888888888", title: "1944 循环的征服者",
      collectionId: "66666666-6666-4666-8666-666666666666", collectionName: "飞机街机",
      targetPlatformInstanceId: platform.id, targetPlatformInstanceName: "FBNeo 游戏",
      metadataRelativePath: "metadata.pegasus.txt", executionState: "COMMIT_FAILED", contentKind: "SINGLE_FILE",
      tags: [],
      media: { cover: "READY", video: "READY" }, warnings: [], discoveryCode: null,
      errorCode: "PEGASUS_LIBRARY_IMPORT_FAILED", retryable: true, publishedGameId: null, existingGameId: null,
      reviewItemId: null,
      existingMatches: [], updatedAtMs: 2, runtimeCheck: null,
      failureDetails: {
        schemaVersion: 1, stage: "LIBRARY_IMPORT", operation: "CREATE_SERVER_SOURCE",
        causeCode: "SOURCE_FILE_LIMIT_EXCEEDED",
        technicalDetail: "Pegasus assembled 109 source files for one Arcade item; library import accepts at most 64.",
        relativePath: "1944j.zip", observedFileCount: 109, allowedFileCount: 64,
        libraryImportJobId: null, libraryImportItemId: null,
      },
    };
    const result = summary("PARTIAL_FAILURE", 4, { retryable: true, completedAtMs: 3 });
    const user = userEvent.setup();

    render(<PegasusImportDetailManager initialSummary={result} initialItems={{ items: [failed], nextCursor: null }} collections={[]} roots={[root]} platformInstances={[platform]} initialFilters={{ query: "", outcome: "", warning: "", collectionId: "" }} />);

    expect(screen.getAllByText("Arcade companion 候选数量超过内部上限")[0]).toBeVisible();
    await user.click(screen.getByText("查看具体原因与处理建议"));
    expect(screen.getByText("SOURCE_FILE_LIMIT_EXCEEDED")).toBeVisible();
    expect(screen.getByText("CREATE_SERVER_SOURCE")).toBeVisible();
    expect(screen.getByText("109 / 上限 64")).toBeVisible();
    expect(screen.getByText("1944j.zip")).toBeVisible();
    expect(screen.getByText(/Pegasus assembled 109 source files/)).toBeVisible();
  });

  it("routes a prepared item into the scoped review queue without a bulk approval action", () => {
    const reviewItem: PegasusItem = {
      id: "99999999-9999-4999-8999-999999999999", title: "Review Fixture",
      collectionId: "66666666-6666-4666-8666-666666666666", collectionName: "FC",
      targetPlatformInstanceId: platform.id, targetPlatformInstanceName: platform.name,
      metadataRelativePath: "metadata.pegasus.txt", executionState: "REVIEW_PENDING", contentKind: "SINGLE_FILE",
      tags: [],
      media: { cover: "READY", video: "READY" }, warnings: [], discoveryCode: null, errorCode: null,
      failureDetails: null, runtimeCheck: { status: "READY", code: "READY", coreId: "fceumm", coreName: "FCEUmm", machine: null, missingEntries: [], mismatchedEntries: [], dependencies: [], bios: [], missingDiscs: [] },
      retryable: false, reviewItemId: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", publishedGameId: null,
      existingGameId: null, existingMatches: [], updatedAtMs: 2,
    };
    const result = summary("COMPLETED", 5, { completedAtMs: 3, counts: { ...summary("COMPLETED", 5).counts, reviewPending: 1 } });

    render(<PegasusImportDetailManager initialSummary={result} initialItems={{ items: [reviewItem], nextCursor: null }} collections={[]} roots={[root]} platformInstances={[platform]} initialFilters={{ query: "", outcome: "", warning: "", collectionId: "" }} />);

    expect(screen.getByRole("link", { name: "逐项审核 1 个游戏" })).toHaveAttribute("href", `/admin/reviews?pegasusImportId=${result.id}`);
    expect(screen.getByRole("link", { name: "审核并决定" })).toHaveAttribute("href", expect.stringContaining(`/admin/reviews/${reviewItem.reviewItemId}`));
    expect(screen.queryByRole("button", { name: /批量/ })).not.toBeInTheDocument();
    expect(screen.getByText("内容已准备好，但尚未进入游戏库")).toBeVisible();
  });
});
