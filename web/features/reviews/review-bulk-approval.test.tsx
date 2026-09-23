import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ReviewBulkApproval } from "./review-bulk-approval";

const navigation = { refresh: vi.fn() };
vi.mock("next/navigation", () => ({ useRouter: () => navigation }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "user-1" } } }) }));

const queued = {
  bulkApprovalId: "01990000-0000-7000-8000-000000000002",
  state: "QUEUED", initialPendingCount: 3, scannedCount: 0, publishedCount: 0,
  skippedChangedCount: 0, skippedDuplicateCount: 0, skippedNotReadyCount: 0,
  lastErrorCode: null,
};

function respond(body: unknown) { return { ok: true, status: 200, json: async () => body }; }

function renderApproval(restoreBulkApprovalId?: string) {
  const root = document.createElement("div");
  root.id = "review-bulk-status-root";
  document.body.append(root);
  return render(<ReviewBulkApproval restoreBulkApprovalId={restoreBulkApprovalId} />);
}

afterEach(() => {
  cleanup();
  document.querySelector("#review-bulk-status-root")?.remove();
  sessionStorage.clear();
  window.history.replaceState({}, "", "/");
  navigation.refresh.mockClear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("ReviewBulkApproval", () => {
  it("discovers the global active task and disables a second creation", async () => {
    const fetchMock = vi.fn().mockResolvedValue(respond({ activeBulkApproval: queued }));
    vi.stubGlobal("fetch", fetchMock);
    renderApproval();
    expect(await screen.findByRole("heading", { name: "正在快速审批" })).toBeVisible();
    expect(screen.getByRole("button", { name: "正在快速审批" })).toBeDisabled();
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/admin/review-bulk-approvals/active", { cache: "no-store" });
  });

  it("creates one global task without a preview or filtered scope", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn().mockResolvedValueOnce(respond({ activeBulkApproval: null }))
      .mockResolvedValueOnce(respond(queued));
    vi.stubGlobal("fetch", fetchMock);
    renderApproval();
    await user.click(await screen.findByRole("button", { name: "快速审批全部待审" }));
    expect(await screen.findByRole("heading", { name: "正在快速审批" })).toBeVisible();
    const create = fetchMock.mock.calls[1];
    expect(create?.[0]).toBe("/api/v1/admin/review-bulk-approvals");
    expect(JSON.parse(String((create?.[1] as RequestInit).body))).toEqual({});
    expect(window.location.search).toContain("bulkApprovalId=" + queued.bulkApprovalId);
  });

  it("recovers task progress after a page refresh", async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(respond({ activeBulkApproval: queued }));
    vi.stubGlobal("fetch", fetchMock);
    renderApproval(queued.bulkApprovalId);
    expect(await screen.findByText(/创建时待审约 3 项/)).toBeVisible();
    expect(screen.getByRole("button", { name: "正在快速审批" })).toBeDisabled();
  });

  it("shows a completed task and refreshes the review queue", async () => {
    const done = { ...queued, state: "COMPLETED", scannedCount: 3, publishedCount: 2, skippedNotReadyCount: 1 };
    const fetchMock = vi.fn().mockResolvedValueOnce(respond({ activeBulkApproval: null }))
      .mockResolvedValueOnce(respond(done));
    vi.stubGlobal("fetch", fetchMock);
    renderApproval(queued.bulkApprovalId);
    expect(await screen.findByRole("heading", { name: "快速审批已完成" })).toBeVisible();
    expect(screen.getByText("继续待审").parentElement).toHaveTextContent("1");
    await waitFor(() => expect(navigation.refresh).toHaveBeenCalled());
  });
});
