import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ReviewQueue, type ReviewQueueItem } from "./review-queue";

vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "user-1" } } }) }));

const item: ReviewQueueItem = {
  itemId: "item-1", reviewVersion: 1, importJobId: "job-secret-looking-id", sourceDisplayName: "1941.zip",
  draftTitle: "1941: Counter Attack", platformInstance: { id: "arcade", name: "FBNeo 游戏" },
  validationStatus: "READY", validationJobId: null, blockerCodes: [], candidateCount: 1, updatedAtMs: 1_786_000_000_000,
  sourceTotalSizeBytes: 4_194_304, sourceMd5: "0123456789abcdef0123456789abcdef", coverUrl: "/api/v1/admin/review-assets/cover-1",
};

afterEach(() => { cleanup(); sessionStorage.clear(); });

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
});
