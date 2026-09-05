import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ServerImportDetailManager,
  ServerImportManager,
  type ServerImportDetail,
  type ServerImportList,
  type ServerImportRoot,
  type ServerImportSummary,
} from "./server-import-manager";

const router = vi.hoisted(() => ({ push: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));

const availableRoot: ServerImportRoot = { id: "pegasus", label: "Pegasus BIOS", status: "AVAILABLE" };

function summary(overrides: Partial<ServerImportSummary> = {}): ServerImportSummary {
  return {
    id: "10000000-0000-4000-8000-000000000001",
    kind: "BIOS_DIRECTORY",
    root: { id: "pegasus", label: "Pegasus BIOS" },
    sourceRelativePath: "BIOS",
    replaceIfBetter: false,
    state: "COMPLETED",
    phase: "QUEUEING_REVALIDATION",
    counts: { catalogItems: 2, candidates: 2, evaluatedItems: 2, imported: 1, matched: 1, warnings: 0, notFound: 1, skipped: 0, conflicts: 0, failed: 0, cancelled: 0 },
    jobId: "20000000-0000-4000-8000-000000000002",
    createdBy: { id: "30000000-0000-4000-8000-000000000003", displayName: "Admin" },
    lastErrorCode: null,
    version: 1,
    createdAtMs: 1,
    updatedAtMs: 2,
    completedAtMs: 3,
    ...overrides,
  };
}

function item(requirementId: string, logicalName = `${requirementId}.bin`): ServerImportDetail["items"][number] {
  return {
    requirementId,
    coreId: "mgba",
    coreName: "mGBA",
    providerId: "emulatorjs",
    targetId: "mgba",
    logicalName,
    requirementMode: "REQUIRED",
    sourceKind: "STATIC",
    state: "IMPORTED_MATCHED",
    candidateCount: 1,
    matchMethod: "EXACT_HASH",
    outcomeCode: "IMPORTED_MATCHED",
    selectedRelativePath: `BIOS/${logicalName}`,
    previousInstallationStatus: null,
    newInstallationStatus: "MATCHED",
    replaced: false,
  };
}

function detail(items: ServerImportDetail["items"] = [item("gba")], nextCursor: string | null = null): ServerImportDetail {
  return { summary: summary({ counts: { ...summary().counts, catalogItems: 2 } }), items, nextCursor };
}

function imports(items: ServerImportSummary[] = [], nextCursor: string | null = null): ServerImportList {
  return { items, nextCursor };
}

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
}

function requestAt(mock: ReturnType<typeof vi.fn>, index: number) {
  return mock.mock.calls[index]?.[0] as Request;
}

afterEach(() => {
  cleanup();
  router.push.mockReset();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("ServerImportManager", () => {
  it("presents EmulationStation as a third equal server import capability", () => {
    render(<ServerImportManager initialRoots={[availableRoot]} initialImports={imports()} />);

    expect(screen.getByRole("heading", { name: "扫描并导入 BIOS" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "扫描并准备审核事项" })).toBeVisible();
    expect(screen.getByRole("heading", { name: "扫描 gamelist.xml 并准备审核" })).toBeVisible();
    expect(screen.getByText(/不执行 command、emulator 或 core/)).toBeVisible();
  });

  it("does not offer an import when every configured root is unavailable", () => {
    render(<ServerImportManager initialRoots={[{ ...availableRoot, status: "UNAVAILABLE" }]} initialImports={imports()} />);
    expect(screen.getByRole("button", { name: "选择目录并开始" })).toBeDisabled();
    expect(screen.getByText("不可用")).toBeVisible();
  });

  it("paginates the root browser without exposing an absolute host path", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ rootId: "pegasus", path: "", items: [{ name: "BIOS", relativePath: "BIOS" }], nextCursor: "next-directories" }))
      .mockResolvedValueOnce(jsonResponse({ rootId: "pegasus", path: "", items: [{ name: "More", relativePath: "More" }], nextCursor: null }));
    vi.stubGlobal("fetch", fetchMock);
    render(<ServerImportManager initialRoots={[availableRoot]} initialImports={imports()} initialOpen />);

    expect(await screen.findByRole("button", { name: /BIOS/ })).toBeVisible();
    expect(screen.queryByText("/srv/private-rom-library")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "加载更多目录" }));
    expect(await screen.findByRole("button", { name: /More/ })).toBeVisible();
    expect(requestAt(fetchMock, 1).url).toContain("cursor=next-directories");
  });

  it("creates an asynchronous BIOS import with the selected replacement policy", async () => {
    const user = userEvent.setup();
    const created = summary({ state: "QUEUED", phase: null, completedAtMs: null, replaceIfBetter: true, sourceRelativePath: "" });
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ rootId: "pegasus", path: "", items: [], nextCursor: null }))
      .mockResolvedValueOnce(jsonResponse(created, 202));
    vi.stubGlobal("fetch", fetchMock);
    render(<ServerImportManager initialRoots={[availableRoot]} initialImports={imports()} initialOpen />);

    await screen.findByText("当前目录没有可进入的子目录。");
    await user.click(screen.getByRole("checkbox", { name: /允许使用更优候选替换已有 BIOS/ }));
    await user.click(screen.getByRole("button", { name: "开始异步导入" }));

    expect(router.push).toHaveBeenCalledWith(`/admin/imports/server/${created.id}`);
    const request = requestAt(fetchMock, 1);
    expect(request.method).toBe("POST");
    expect(await request.clone().json()).toEqual({ kind: "BIOS_DIRECTORY", rootId: "pegasus", sourceRelativePath: "", replaceIfBetter: true });
  });

  it("loads complete history on the same page with cursor deduplication", async () => {
    const user = userEvent.setup();
    const first = summary();
    const second = summary({ id: "10000000-0000-4000-8000-000000000009", sourceRelativePath: "Arcade" });
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ items: [first, second], nextCursor: null }));
    vi.stubGlobal("fetch", fetchMock);
    render(<ServerImportManager initialRoots={[availableRoot]} initialImports={imports([first], "history-cursor")} />);

    await user.click(screen.getByRole("button", { name: "查看全部历史" }));
    expect(await screen.findByText("Pegasus BIOS / Arcade")).toBeVisible();
    expect(screen.getAllByText("Pegasus BIOS / BIOS")).toHaveLength(1);
    expect(requestAt(fetchMock, 0).url).toContain("cursor=history-cursor");
    expect(requestAt(fetchMock, 0).url).toContain("limit=20");
  });
});

describe("ServerImportDetailManager", () => {
  it("appends a cursor page once and keeps existing BIOS results", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(detail([item("psx", "scph5501.bin")])));
    vi.stubGlobal("fetch", fetchMock);
    render(<ServerImportDetailManager initialDetail={detail([item("gba", "gba_bios.bin")], "item-cursor")} />);

    await user.click(screen.getByRole("button", { name: "加载更多结果" }));
    expect(await screen.findByText("scph5501.bin")).toBeVisible();
    expect(screen.getByText("gba_bios.bin")).toBeVisible();
    expect(requestAt(fetchMock, 0).url).toContain("cursor=item-cursor");
    expect(requestAt(fetchMock, 0).url).toContain("limit=50");
  });

  it("sends result filters to the server and opens ranked candidate evidence", async () => {
    const user = userEvent.setup();
    const filtered = detail([item("gba", "gba_bios.bin")]);
    const candidate = {
      id: "40000000-0000-4000-8000-000000000004",
      relativePath: "BIOS/gba_bios.bin",
      basename: "gba_bios.bin",
      associationKind: "EXACT_NAME" as const,
      sizeBytes: 16384,
      md5: "a".repeat(32), sha1: "b".repeat(40), sha256: "c".repeat(64), crc32: "12345678",
      state: "SELECTED" as const,
      rankOrdinal: 1,
      notSelectedReason: null,
    };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(filtered))
      .mockResolvedValueOnce(jsonResponse({ items: [candidate], nextCursor: null }));
    vi.stubGlobal("fetch", fetchMock);
    render(<ServerImportDetailManager initialDetail={filtered} />);

    await user.type(screen.getByRole("searchbox", { name: "搜索 BIOS 或核心" }), "gba");
    await user.selectOptions(screen.getByLabelText("结果"), "IMPORTED_MATCHED");
    await user.selectOptions(screen.getByLabelText("匹配方式"), "EXACT_HASH");
    await user.click(screen.getByRole("button", { name: "应用筛选" }));
    expect(requestAt(fetchMock, 0).url).toContain("q=gba");
    expect(requestAt(fetchMock, 0).url).toContain("outcome=IMPORTED_MATCHED");
    expect(requestAt(fetchMock, 0).url).toContain("matchMethod=EXACT_HASH");

    await user.click(screen.getByRole("button", { name: "查看候选（1）" }));
    expect(await screen.findByRole("alertdialog", { name: "gba_bios.bin 候选排序" })).toBeVisible();
    expect(screen.getByText("BIOS/gba_bios.bin")).toBeVisible();
    expect(requestAt(fetchMock, 1).url).toContain("/bios-items/gba/candidates");
  });
});
