import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ReviewQueue, type ReviewQueueItem } from "./review-queue";

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
    expect(screen.getByText(/MD5 012345/)).toHaveAttribute("title", `MD5 ${item.sourceMd5}`);
    expect(screen.queryByText("批次详情")).not.toBeInTheDocument();
    expect(screen.queryByText(item.importJobId)).not.toBeInTheDocument();
  });
});
