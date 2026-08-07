import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SaveManager, type SaveItem } from "./save-manager";

const router = vi.hoisted(() => ({ refresh: vi.fn(), replace: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));

const save: SaveItem = {
  saveStateId: "save-1", gameId: "game-1", gameTitle: "Metal Slug", name: "最终关",
  version: 3, createdAtMs: 1_786_000_000_000, activeDurationMs: 540_000,
  screenshotUrl: "/content/save-states/save-1/screenshot", core: { id: "fbneo", name: "FinalBurn Neo" },
  availability: { status: "AVAILABLE", reasons: [] },
};

describe("SaveManager", () => {
  beforeEach(() => {
    router.refresh.mockReset(); router.replace.mockReset();
    vi.stubGlobal("fetch", vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => init?.method === "DELETE" ? new Response(null, { status: 204 }) : new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } })));
  });
  afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

  it("renames in place and exposes deletion through the screenshot icon", async () => {
    const user = userEvent.setup();
    render(<SaveManager saves={[save]} />);
    expect(screen.queryByRole("button", { name: "管理存档" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "从这里继续" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "编辑存档“最终关”的名称" }));
    const input = screen.getByLabelText("存档名称");
    await user.clear(input); await user.type(input, "Boss 前");
    await user.click(screen.getByRole("button", { name: "保存" }));
    await waitFor(() => expect(fetch).toHaveBeenCalledWith("/api/v1/saves/save-1", expect.objectContaining({ method: "PATCH", body: JSON.stringify({ name: "Boss 前" }) })));
    expect(router.refresh).toHaveBeenCalledOnce();

    await user.click(screen.getByRole("button", { name: "删除存档“最终关”" }));
    expect(screen.getByRole("alertdialog", { name: "删除这份存档？" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "删除存档" }));
    await waitFor(() => expect(fetch).toHaveBeenCalledWith("/api/v1/saves/save-1", expect.objectContaining({ method: "DELETE" })));
  });
});
