import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ReviewBulkApproval } from "./review-bulk-approval";

const navigation = { refresh: vi.fn() };

vi.mock("next/navigation", () => ({ useRouter: () => navigation }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "user-1" } } }) }));

const counts = {
  matched: 12, strictReady: 7, screenshotOnly: 2, duplicate: 1, attachmentActive: 1, notReadyOrStale: 1, sourceFlagged: 3,
};

const preview = {
  scope: { pegasusImportId: "01990000-0000-7000-8000-000000000001" },
  scopeDigest: "a".repeat(64), candidateManifestDigest: "b".repeat(64), counts, activeBulkApproval: null,
};

const queued = {
  bulkApprovalId: "01990000-0000-7000-8000-000000000002",
  jobId: "01990000-0000-7000-8000-000000000003",
  state: "QUEUED", version: 1, scope: preview.scope, initialCounts: counts,
  counts: { candidate: 7, processed: 0, published: 0, skippedDuplicate: 0, skippedChanged: 0, skippedNotReady: 0, failed: 0, cancelled: 0 },
  lastErrorCode: null,
};

afterEach(() => {
  cleanup();
  document.querySelector("#review-bulk-status-root")?.remove();
  sessionStorage.clear();
  window.history.replaceState({}, "", "/");
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function renderApproval(restoreBulkApprovalId?: string) {
  const root = document.createElement("div");
  root.id = "review-bulk-status-root";
  document.body.append(root);
  return render(<ReviewBulkApproval
    values={{ pegasusImportId: preview.scope.pegasusImportId, sort: "UPDATED_DESC" }}
    restoreBulkApprovalId={restoreBulkApprovalId}
  />);
}

describe("ReviewBulkApproval", () => {
  it("previews the complete server scope and explains every excluded class", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => preview });
    vi.stubGlobal("fetch", fetchMock);

    renderApproval();
    await user.click(screen.getByRole("button", { name: "快速审批" }));

    expect(await screen.findByRole("alertdialog", { name: "快速审批可直接发布的游戏" })).toBeVisible();
    expect(screen.getByText("可自动发布").parentElement).toHaveTextContent("7");
    expect(screen.getByText("仅有运行截图人工放行").parentElement).toHaveTextContent("2");
    expect(screen.getByText("发现重复内容").parentElement).toHaveTextContent("1");
    expect(screen.getByText(/hidden\/adult 来源标记需逐项核对/)).toHaveTextContent("3");
    expect(screen.getByRole("button", { name: "确认快速发布 7 个游戏" })).toBeEnabled();
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining("pegasusImportId=01990000-0000-7000-8000-000000000001"),
      { cache: "no-store" },
    );
    expect(String(fetchMock.mock.calls[0]?.[0])).not.toContain("sort=");
  });

  it("keeps an EmulationStation batch as an independent frozen scope", async () => {
    const user = userEvent.setup();
    const emulationStationId = "01990000-0000-7000-8000-000000000099";
    const emulationStationPreview = { ...preview, scope: { emulationStationImportId: emulationStationId } };
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => emulationStationPreview });
    vi.stubGlobal("fetch", fetchMock);
    const root = document.createElement("div");
    root.id = "review-bulk-status-root";
    document.body.append(root);
    render(<ReviewBulkApproval values={{ emulationStationImportId: emulationStationId }} />);

    await user.click(screen.getByRole("button", { name: "快速审批" }));

    expect(await screen.findByText(/EmulationStation 批次 .* 的全部分页结果/)).toBeVisible();
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining(`emulationStationImportId=${emulationStationId}`),
      { cache: "no-store" },
    );
  });

  it("starts a frozen background batch and exposes recoverable progress", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => preview })
      .mockResolvedValueOnce({ ok: true, json: async () => queued });
    vi.stubGlobal("fetch", fetchMock);
    window.history.replaceState({}, "", "/admin/reviews?pegasusImportId=" + preview.scope.pegasusImportId);

    renderApproval();
    await user.click(screen.getByRole("button", { name: "快速审批" }));
    await user.click(await screen.findByRole("button", { name: "确认快速发布 7 个游戏" }));

    expect(await screen.findByRole("heading", { name: "正在快速审批" })).toBeVisible();
    expect(screen.getByText("已处理 0 / 7", { exact: false })).toBeVisible();
    expect(screen.getByRole("button", { name: "查看快速审批进度" })).toBeVisible();
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    const create = fetchMock.mock.calls[1];
    expect(create?.[0]).toBe("/api/v1/admin/review-bulk-approvals");
    expect(JSON.parse(String((create?.[1] as RequestInit).body))).toEqual({
      scope: preview.scope, scopeDigest: preview.scopeDigest,
      candidateManifestDigest: preview.candidateManifestDigest,
    });
    expect(window.location.search).toContain(`bulkApprovalId=${queued.bulkApprovalId}`);
  });

  it("shows preview failures beside the queue instead of dropping the error", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) }));

    renderApproval();
    await user.click(screen.getByRole("button", { name: "快速审批" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("无法计算快速审批范围");
  });

  it("requires confirmation before cancelling unprocessed items", async () => {
    const user = userEvent.setup();
    const cancelled = { ...queued, state: "CANCELLED", version: 2,
      counts: { ...queued.counts, processed: 7, cancelled: 7 } };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, json: async () => queued })
      .mockResolvedValueOnce({ ok: true, json: async () => cancelled })
      .mockResolvedValueOnce({ ok: true, json: async () => ({ items: [] }) });
    vi.stubGlobal("fetch", fetchMock);

    renderApproval(queued.bulkApprovalId);
    await user.click(await screen.findByRole("button", { name: "停止未处理项目" }));

    const dialog = screen.getByRole("alertdialog", { name: "停止快速审批？" });
    expect(dialog).toBeVisible();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    await user.click(within(dialog).getByRole("button", { name: "停止未处理项目" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(3));
    expect(fetchMock.mock.calls[1]?.[0]).toBe(`/api/v1/admin/review-bulk-approvals/${queued.bulkApprovalId}/cancel`);
  });
});
