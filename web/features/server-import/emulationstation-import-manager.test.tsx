import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { EmulationStationImportDetailView } from "./emulationstation-import-detail-view";
import {
  EmulationStationImportDrawer,
  type EmulationStationCollection,
  type EmulationStationGamelist,
  type EmulationStationImportSummary,
  type EmulationStationItem,
} from "./emulationstation-import-manager";

const router = vi.hoisted(() => ({ push: vi.fn(), refresh: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));

function counts(): EmulationStationImportSummary["counts"] {
  return {
    gamelists: 2,
    invalidGamelists: 1,
    collections: 1,
    foldersIgnored: 2,
    games: 3,
    estimatedSourceBytes: 4096,
    mappedCollections: 0,
    skippedCollections: 0,
    skippedMapping: 0,
    processable: 3,
    blocked: 0,
    reviewPending: 0,
    published: 0,
    reviewDiscarded: 0,
    existing: 0,
    failed: 0,
    cancelled: 0,
    mediaWarnings: 1,
    covers: 1,
    videos: 0,
  };
}

function summary(overrides: Partial<EmulationStationImportSummary> = {}): EmulationStationImportSummary {
  return {
    id: "10000000-0000-4000-8000-000000000101",
    root: { id: "roms", label: "ROM 目录" },
    sourceRelativePath: "collections",
    state: "AWAITING_MAPPING",
    phase: null,
    scanJobId: "20000000-0000-4000-8000-000000000102",
    importJobId: null,
    counts: counts(),
    mappingVersion: 1,
    version: 1,
    createdBy: { id: "30000000-0000-4000-8000-000000000103", displayName: "Admin" },
    lastErrorCode: null,
    retryable: false,
    createdAtMs: 1,
    updatedAtMs: 2,
    expiresAtMs: 3,
    completedAtMs: null,
    ...overrides,
  };
}

const collection: EmulationStationCollection = {
  id: "40000000-0000-4000-8000-000000000104",
  gamelistRelativePath: "nes/gamelist.xml",
  relativeDirectory: "nes",
  displayName: "NES 清单",
  gameCount: 3,
  issueCount: 1,
  folderEntryCount: 2,
  hiddenGameCount: 1,
  adultGameCount: 0,
  extensionSummary: [{ extension: ".nes", count: 3 }],
  extensionOtherCount: 0,
  mappingAction: null,
  targetPlatformInstanceId: null,
  targetPlatformInstanceName: null,
  targetDefaultCoreId: null,
  targetDefaultCoreName: null,
  tagSnapshot: [],
};

const gamelists: EmulationStationGamelist[] = [
  {
    relativePath: "nes/gamelist.xml",
    parseState: "VALID",
    errorCode: null,
    gameCount: 3,
    folderCount: 2,
    providerPresent: true,
    ignoredFieldNames: ["command"],
    ignoredFieldOtherCount: 0,
    createdAtMs: 1,
  },
  {
    relativePath: "broken/gamelist.xml",
    parseState: "INVALID",
    errorCode: "EMULATIONSTATION_XML_INVALID",
    gameCount: 0,
    folderCount: 0,
    providerPresent: false,
    ignoredFieldNames: [],
    ignoredFieldOtherCount: 0,
    createdAtMs: 1,
  },
];

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
}

function requestAt(mock: ReturnType<typeof vi.fn>, index: number) {
  return mock.mock.calls[index]?.[0] as Request;
}

afterEach(() => {
  cleanup();
  router.push.mockReset();
  router.refresh.mockReset();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("EmulationStationImportDrawer", () => {
  it("requires an explicit mapping, preserves source warnings, and starts the frozen plan", async () => {
    const user = userEvent.setup();
    const mappedSummary = summary({ version: 2, mappingVersion: 2, counts: { ...counts(), mappedCollections: 1 } });
    const queuedSummary = summary({ state: "QUEUED", version: 3, mappingVersion: 2, importJobId: "50000000-0000-4000-8000-000000000105" });
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(summary()))
      .mockResolvedValueOnce(jsonResponse({ items: [collection], nextCursor: null }))
      .mockResolvedValueOnce(jsonResponse({ items: gamelists, nextCursor: null }))
      .mockResolvedValueOnce(jsonResponse(mappedSummary))
      .mockResolvedValueOnce(jsonResponse(queuedSummary, 202));
    vi.stubGlobal("fetch", fetchMock);

    render(<EmulationStationImportDrawer
      open
      roots={[{ id: "roms", label: "ROM 目录", status: "AVAILABLE" }]}
      platformInstances={[{ id: "platform-1", name: "NES 游戏", platformName: "Nintendo Entertainment System", defaultCoreId: "fceumm", defaultCoreName: "FCEUmm", enabled: true }]}
      resumablePlan={summary()}
      onClose={vi.fn()}
      onStarted={vi.fn()}
    />);

    expect(await screen.findByText("NES 清单")).toBeVisible();
    expect(screen.getByText("nes/gamelist.xml")).toBeVisible();
    expect(screen.getByText("1 份清单无法解析")).toBeVisible();
    expect(screen.getByText(/hidden 1/)).toBeVisible();
    expect(screen.getByRole("button", { name: "确认映射" })).toBeDisabled();

    await user.selectOptions(screen.getByLabelText("NES 清单 处理方式"), "IMPORT:platform-1");
    await user.click(screen.getByRole("button", { name: "确认映射" }));

    expect(await screen.findByText("全部进入待审核，不会自动发布")).toBeVisible();
    const mappingRequest = requestAt(fetchMock, 3);
    expect(mappingRequest.method).toBe("PUT");
    expect(await mappingRequest.clone().json()).toEqual({
      mappings: [{ collectionId: collection.id, action: "IMPORT", platformInstanceId: "platform-1", tagIds: [] }],
    });

    await user.click(screen.getByRole("button", { name: "开始准备审核事项" }));
    await waitFor(() => expect(router.push).toHaveBeenCalledWith(`/admin/imports/server/emulationstation/${queuedSummary.id}`));
    expect(requestAt(fetchMock, 4).method).toBe("POST");
  });

  it("stops mapping with a clear route when no enabled game directory exists", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(summary()))
      .mockResolvedValueOnce(jsonResponse({ items: [collection], nextCursor: null }))
      .mockResolvedValueOnce(jsonResponse({ items: gamelists, nextCursor: null }));
    vi.stubGlobal("fetch", fetchMock);

    render(
      <EmulationStationImportDrawer
        open
        roots={[{ id: "roms", label: "ROM 目录", status: "AVAILABLE" }]}
        platformInstances={[]}
        resumablePlan={summary()}
        onClose={vi.fn()}
        onStarted={vi.fn()}
      />,
    );

    expect(await screen.findByRole("heading", { name: "还没有游戏目录" })).toBeVisible();
    expect(screen.getByRole("link", { name: "前往游戏目录" })).toHaveAttribute("href", "/admin/platform-instances");
  });
});

describe("EmulationStationImportDetailView", () => {
  it("shows source flags, payload release, gamelist evidence, and a scoped review route", () => {
    const item: EmulationStationItem = {
      id: "60000000-0000-4000-8000-000000000106", title: "Flagged Game", collectionId: collection.id,
      collectionName: collection.displayName, targetPlatformInstanceId: "platform-1", targetPlatformInstanceName: "NES 游戏",
      gamelistRelativePath: collection.gamelistRelativePath, sourceFlags: { hidden: true, adult: false, kidGame: true },
      executionState: "REVIEW_PENDING", payloadState: "RELEASED", payloadReleaseJobId: null, contentKind: "SINGLE_FILE",
      tags: [], media: { cover: "READY", video: "MISSING" }, warnings: [{ code: "EMULATIONSTATION_MEDIA_MISSING", field: "video" }],
      discoveryCode: null, errorCode: null, failureDetails: null, runtimeCheck: null, retryable: false,
      reviewItemId: "70000000-0000-4000-8000-000000000107", publishedGameId: null, existingGameId: null, existingMatches: [], updatedAtMs: 4,
    };
    const completed = summary({ state: "COMPLETED", counts: { ...counts(), reviewPending: 1 }, completedAtMs: 4 });
    render(<EmulationStationImportDetailView
      summary={completed} items={[item]} nextCursor={null} draft={{ query: "", outcome: "", warning: "", collectionId: "" }}
      collections={[collection]} gamelists={gamelists} busy={false} error="" cancelOpen={false} deleteOpen={false}
      mappingOpen={false} mappingDrawer={null} onDraft={vi.fn()} onApplyFilters={vi.fn()} onCancelOpen={vi.fn()}
      onDeleteOpen={vi.fn()} onCancel={vi.fn()} onDelete={vi.fn()} onRetry={vi.fn()} onMappingOpen={vi.fn()}
      onLoadMore={vi.fn()} onDismissError={vi.fn()}
    />);

    expect(screen.getByLabelText("来源标记：hidden、kidgame")).toBeVisible();
    expect(screen.getByText("源文件已清理")).toBeVisible();
    expect(screen.getByText(/2 份 Gamelist 扫描结果/)).toBeVisible();
    expect(screen.getByRole("link", { name: "打开这批审核队列" })).toHaveAttribute(
      "href",
      `/admin/reviews?emulationStationImportId=${completed.id}`,
    );
  });
});
