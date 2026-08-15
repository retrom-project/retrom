import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ReviewQueue, ReviewQueueRecovery, type ReviewQueueItem } from "./review-queue";

vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "user-1" } } }) }));

const item: ReviewQueueItem = {
  itemId: "item-1", reviewVersion: 1, importJobId: "job-secret-looking-id", sourceDisplayName: "1941.zip",
  draftTitle: "1941: Counter Attack", platformInstance: { id: "arcade", name: "FBNeo 游戏" },
  validationStatus: "READY", validationJobId: null, blockerCodes: [], candidateCount: 1, updatedAtMs: 1_786_000_000_000,
  sourceTotalSizeBytes: 4_194_304, sourceMd5: "0123456789abcdef0123456789abcdef", coverUrl: "/api/v1/admin/review-assets/cover-1",
};

afterEach(() => {
  cleanup();
  sessionStorage.clear();
  window.history.replaceState({}, "", "/");
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("ReviewQueue", () => {
  it("shows a compact cover and file facts without batch UUID details", () => {
    render(<ReviewQueue initial={{ items: [item], nextCursor: null }} values={{}} />);
    expect(screen.getByAltText("1941: Counter Attack 封面缩略图")).toBeInTheDocument();
    expect(screen.getByText("4.0 MiB")).toBeInTheDocument();
    expect(screen.getByText("MD5 0123…")).toHaveAttribute("title", `MD5 ${item.sourceMd5}`);
    expect(screen.queryByText("批次详情")).not.toBeInTheDocument();
    expect(screen.queryByText(item.importJobId)).not.toBeInTheDocument();
  });

  it("uses the summary chips as real filters over the loaded queue", async () => {
    const user = userEvent.setup();
    const blocked = { ...item, itemId: "item-2", draftTitle: "Blocked game", validationStatus: "BLOCKED", blockerCodes: ["LAUNCH_BIOS_MISSING"], candidateCount: 0 };
    render(<ReviewQueue initial={{ items: [item, blocked], nextCursor: null }} values={{}} />);

    await user.click(screen.getByRole("button", { name: "运行异常 1" }));
    expect(screen.getByText("Blocked game")).toBeVisible();
    expect(screen.queryByText(item.draftTitle)).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "未找到信息 1" }));
    expect(screen.getByText("Blocked game")).toBeVisible();
    expect(screen.getByText("当前筛选显示 1 / 已加载 2 条")).toBeVisible();
  });

  it("treats Pegasus metadata as reviewable source information instead of a scrape miss", () => {
    const pegasus = { ...item, sourceKind: "PEGASUS" as const, sourceLabel: "FC", candidateCount: 0 };
    render(<ReviewQueue initial={{ items: [pegasus], nextCursor: null }} values={{ pegasusImportId: "batch-1" }} />);

    expect(screen.getByText("已读取 Pegasus 信息")).toBeVisible();
    expect(screen.getByText("等待管理员核对")).toBeVisible();
    expect(screen.getByRole("button", { name: "未找到信息 0" })).toBeVisible();
  });

  it("ignores a persisted queue after a stale review link is recovered", async () => {
    const staleItem = { ...item, itemId: "stale-item", draftTitle: "Already processed game" };
    sessionStorage.setItem("retrom:v2:user:user-1:reviews:queue:", JSON.stringify({
      items: [staleItem], nextCursor: null, scrollY: 0,
    }));

    render(<ReviewQueue initial={{ items: [item], nextCursor: null }} values={{}} resetPersisted />);

    await act(async () => {
      await new Promise<void>((resolve) => window.requestAnimationFrame(() => resolve()));
    });
    expect(screen.getByText(item.draftTitle)).toBeVisible();
    expect(screen.queryByText(staleItem.draftTitle)).not.toBeInTheDocument();
  });

  it("clears the affected queue cache and consumes a stale-review notice", async () => {
    const storageKey = "retrom:v2:user:user-1:reviews:queue:importJobId=job-1";
    sessionStorage.setItem(storageKey, "stale queue");
    window.history.replaceState({}, "", "/admin/reviews?importJobId=job-1&reviewNotice=stale");

    render(<ReviewQueueRecovery active values={{ importJobId: "job-1" }} />);

    expect(screen.getByRole("status")).toHaveTextContent("审核条目已处理或不再可用");
    await waitFor(() => expect(sessionStorage.getItem(storageKey)).toBeNull());
    expect(window.location.pathname + window.location.search).toBe("/admin/reviews?importJobId=job-1");
  });

  it("loads only one page while the sentinel remains inside the preload area", async () => {
    const observers: Array<(entries: IntersectionObserverEntry[]) => void> = [];
    class IntersectionObserverMock {
      constructor(callback: IntersectionObserverCallback) {
        observers.push((entries) => callback(entries, this as unknown as IntersectionObserver));
      }
      observe() {}
      disconnect() {}
      unobserve() {}
      takeRecords() { return []; }
      readonly root = null;
      readonly rootMargin = "320px 0px";
      readonly thresholds = [0];
    }
    vi.stubGlobal("IntersectionObserver", IntersectionObserverMock);
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ items: [{ ...item, itemId: "item-2", draftTitle: "Second game" }], nextCursor: "cursor-2" }),
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<ReviewQueue initial={{ items: [item], nextCursor: "cursor-1" }} values={{ sort: "UPDATED_ASC" }} />);
    expect(observers).toHaveLength(1);

    await act(async () => {
      observers[0]([{ isIntersecting: true } as IntersectionObserverEntry]);
    });
    await screen.findByText("Second game");
    await waitFor(() => expect(observers.length).toBeGreaterThan(1));

    await act(async () => {
      observers.at(-1)?.([{ isIntersecting: true } as IntersectionObserverEntry]);
    });

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/admin/reviews?sort=UPDATED_ASC&cursor=cursor-1&limit=20",
      { cache: "no-store" },
    );

    await act(async () => {
      observers.at(-1)?.([{ isIntersecting: false } as IntersectionObserverEntry]);
      observers.at(-1)?.([{ isIntersecting: true } as IntersectionObserverEntry]);
    });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    expect(fetchMock).toHaveBeenLastCalledWith(
      "/api/v1/admin/reviews?sort=UPDATED_ASC&cursor=cursor-2&limit=20",
      { cache: "no-store" },
    );
  });
});
