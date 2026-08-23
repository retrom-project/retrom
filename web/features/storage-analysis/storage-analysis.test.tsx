import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { StorageCategoryCode, StorageSnapshot } from "./model";
import { StorageAnalysis } from "./storage-analysis";

const codes: StorageCategoryCode[] = [
  "GAME_CONTENT", "BIOS", "SAVES", "MEDIA", "WORKFLOW", "RUNTIME_SNAPSHOT",
  "SHARED_DURABLE", "OTHER_REFERENCED", "UNREFERENCED",
];

function snapshot(registeredBytes = "100", generatedAtMs = 1_800_000_000_000): StorageSnapshot {
  return {
    scope: "REGISTERED_CAS_PAYLOAD_V1",
    generatedAtMs,
    totals: { registeredBytes, protectedBytes: registeredBytes, unreferencedBytes: "0", blobCount: registeredBytes === "0" ? 0 : 1 },
    categories: codes.map((code, index) => ({ code, bytes: index === 0 ? registeredBytes : "0", blobCount: index === 0 && registeredBytes !== "0" ? 1 : 0 })),
    details: {
      saveStates: { activeCount: 0, deletedCount: 0, stateReferenceBytes: "0", screenshotReferenceBytes: "0" },
      cleanupCandidates: { blobCount: 0, bytes: "0" },
    },
    excluded: [
      "DATABASE_FILES", "UPLOAD_PARTS", "JOB_SCRATCH", "DEPENDENCY_ROOT",
      "FILESYSTEM_OVERHEAD", "UNREGISTERED_ORPHANS", "VOLUME_FREE_SPACE",
    ],
  };
}

function snapshotWithUnreferenced(unreferencedBytes = "60", blobCount = 2): StorageSnapshot {
  const value = snapshot("100");
  value.totals = { registeredBytes: "100", protectedBytes: "40", unreferencedBytes, blobCount: 3 };
  value.categories = value.categories.map((category) => {
    if (category.code === "GAME_CONTENT") {return { ...category, bytes: "40", blobCount: 1 };}
    if (category.code === "UNREFERENCED") {return { ...category, bytes: unreferencedBytes, blobCount };}
    return category;
  });
  value.details.cleanupCandidates = { blobCount, bytes: unreferencedBytes };
  return value;
}

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("StorageAnalysis", () => {
  it("announces loading, renders all categories, and refreshes the snapshot", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(snapshot()))
      .mockResolvedValueOnce(jsonResponse(snapshot("2048", 1_800_000_100_000)));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<StorageAnalysis />);

    expect(screen.getByRole("status", { name: "正在读取容量分析" })).toBeInTheDocument();
    expect(await screen.findByLabelText("已登记 CAS，精确值 100 bytes")).toHaveTextContent("100 B");
    expect(screen.getByRole("heading", { name: "容量分析" })).toBeInTheDocument();
    const breakdown = screen.getByRole("region", { name: "按用途分析" });
    for (const label of ["ROM 与游戏内容", "BIOS 与运行 bundle", "存档", "游戏媒体", "导入与审核工作区", "运行快照", "跨领域共享", "其他受保护数据", "未引用、等待回收"]) {
      expect(within(breakdown).getByRole("heading", { name: label })).toBeInTheDocument();
    }
    expect(screen.getByText("仅计算已登记 CAS payload")).toBeInTheDocument();
    expect(screen.getByText(/已登记总量在默认 7 天宽限期后才会下降/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "立即清理" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "刷新分析" }));
    expect(await screen.findByLabelText("已登记 CAS，精确值 2048 bytes")).toHaveTextContent("2 KiB");
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/storage-analysis", expect.objectContaining({ cache: "no-store", credentials: "same-origin" }));
  });

  it("keeps the previous snapshot and identifies a failed refresh", async () => {
    vi.stubGlobal("fetch", vi.fn()
      .mockResolvedValueOnce(jsonResponse(snapshot()))
      .mockResolvedValueOnce(jsonResponse({ error: { message: "数据库繁忙" } }, 500)));
    const user = userEvent.setup();
    render(<StorageAnalysis />);

    await screen.findByLabelText("已登记 CAS，精确值 100 bytes");
    await user.click(screen.getByRole("button", { name: "刷新分析" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("操作失败，继续显示");
    expect(screen.getByLabelText("已登记 CAS，精确值 100 bytes")).toBeInTheDocument();
  });

  it("offers a retry after the initial request fails", async () => {
    vi.stubGlobal("fetch", vi.fn()
      .mockResolvedValueOnce(jsonResponse({ error: { message: "暂时不可用" } }, 500))
      .mockResolvedValueOnce(jsonResponse(snapshot("0"))));
    const user = userEvent.setup();
    render(<StorageAnalysis />);

    expect(await screen.findByRole("heading", { name: "容量分析暂时不可用" })).toBeInTheDocument();
    expect(screen.getByText("仅计算已登记 CAS payload")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "重新读取" }));
    expect(await screen.findByRole("heading", { name: "还没有已登记的 CAS 数据" })).toBeInTheDocument();
  });

  it("confirms and schedules immediate cleanup without asking for typed input", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(snapshotWithUnreferenced()))
      .mockResolvedValueOnce(jsonResponse({
        scheduledBlobCount: 2, scheduledBytes: "60", acceptedAtMs: 1_800_000_000_100,
      }, 202))
      .mockResolvedValueOnce(jsonResponse(snapshot("40", 1_800_000_000_200)));
    vi.stubGlobal("fetch", fetchMock);
    const user = userEvent.setup();
    render(<StorageAnalysis />);

    const cleanupButton = await screen.findByRole("button", { name: "立即清理" });
    expect(cleanupButton).toBeEnabled();
    await user.click(cleanupButton);
    const dialog = screen.getByRole("alertdialog", { name: "立即清理未引用数据？" });
    expect(within(dialog).getByText(/2 个 Blob/)).toBeInTheDocument();
    expect(within(dialog).getByText(/60 B/)).toBeInTheDocument();
    expect(within(dialog).queryByRole("textbox")).not.toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "立即清理" }));

    expect(await screen.findByRole("status")).toHaveTextContent("已安排立即清理 2 个 Blob（60 B）");
    expect(fetchMock).toHaveBeenNthCalledWith(2, "/api/v1/admin/storage-cleanups", expect.objectContaining({
      method: "POST",
      credentials: "same-origin",
      headers: expect.objectContaining({ "Idempotency-Key": expect.any(String) }),
    }));
    expect(await screen.findByLabelText("已登记 CAS，精确值 40 bytes")).toHaveTextContent("40 B");
  });
});
