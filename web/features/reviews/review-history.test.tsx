import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ReviewHistory, type HistoryItem } from "./review-history";

const item: HistoryItem = { reviewEventId: "event-1", importItemId: "item-1", importJobId: "job-1", title: "1941", decision: "APPROVED", reason: "资料已核对", createdAtMs: 1_786_000_000_000 };

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("ReviewHistory", () => {
  it("opens the metadata snapshot instead of exposing technical identifiers", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ reviewEventId: "event-1", eventType: "APPROVED", reason: "资料已核对", createdAtMs: item.createdAtMs, before: { metadata: { title: "1941: Counter Attack", description: "Arcade shooter", developer: "Capcom", releaseYear: 1990 }, selectedAssets: { coverCandidateAssetId: null } } }), { status: 200, headers: { "Content-Type": "application/json" } })));
    const user = userEvent.setup();
    render(<ReviewHistory items={[item]} />);
    expect(screen.queryByText("技术详情")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "查看“1941”的审核快照" }));
    const dialog = await screen.findByRole("alertdialog", { name: "审核完成时的游戏信息" });
    expect(dialog).toHaveTextContent("1941: Counter Attack");
    expect(dialog).toHaveTextContent("Capcom");
    expect(dialog).toHaveTextContent("1990");
  });

  it("falls back to a placeholder when a historical cover is unavailable", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ reviewEventId: "event-1", eventType: "APPROVED", reason: null, createdAtMs: item.createdAtMs, before: { metadata: { title: "1943: The Battle of Midway" }, selectedAssets: { coverCandidateAssetId: "cover-1" } } }), { status: 200, headers: { "Content-Type": "application/json" } })));
    const user = userEvent.setup();
    render(<ReviewHistory items={[item]} />);

    await user.click(screen.getByRole("button", { name: "查看“1941”的审核快照" }));
    fireEvent.error(await screen.findByRole("img", { name: "1943: The Battle of Midway 审核时封面" }));
    expect(screen.getByText("历史封面暂不可用")).toBeInTheDocument();
  });
});
