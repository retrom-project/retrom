import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SaveManager, type SaveItem } from "./save-manager";

const router = vi.hoisted(() => ({ push: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));

const nowMs = new Date(2026, 7, 8, 22, 0).getTime();

function makeSave(overrides: Partial<SaveItem> = {}): SaveItem {
  return {
    saveStateId: "save-1",
    gameId: "game-1",
    gameTitle: "Metal Slug",
    name: "最终关",
    version: 3,
    createdAtMs: new Date(2026, 7, 8, 21, 9, 47).getTime(),
    activeDurationMs: 540_000,
    sizeBytes: Math.round(1.23 * 1024 * 1024),
    screenshotUrl: "/content/save-states/save-1/screenshot",
    core: { id: "fbneo", name: "FinalBurn Neo" },
    platform: { id: "arcade", name: "街机" },
    platformInstance: { id: "fbneo-games", name: "FBNeo 游戏" },
    availability: { status: "AVAILABLE", reasons: [] },
    ...overrides,
  };
}

describe("SaveManager", () => {
  beforeEach(() => {
    router.push.mockReset();
    window.history.replaceState({}, "", "/saves");
    vi.stubGlobal("fetch", vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "DELETE") {return new Response(null, { status: 204 });}
      return Response.json({ name: "Boss 前", version: 4 });
    }));
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("keeps card action menus outside the game-group clipping boundary", () => {
    const source = readFileSync(resolve(process.cwd(), "features/imports/import-review-workflow.css"), "utf8");
    const rule = source.match(/\.save-library-group\s*\{([^}]*)\}/)?.[1] ?? "";

    expect(rule).toContain("overflow: visible");
  });

  it("anchors an opaque size label to the screenshot tray without changing card flow", () => {
    const source = readFileSync(resolve(process.cwd(), "features/imports/import-review-workflow.css"), "utf8");
    const rule = source.match(/\.save-library-size\s*\{([^}]*)\}/)?.[1] ?? "";

    expect(rule).toContain("position: absolute");
    expect(rule).toContain("right: 8px");
    expect(rule).toContain("bottom: 8px");
    expect(rule).toContain("height: 22px");
    expect(rule).toContain("padding: 1px 7px 0");
    expect(rule).toContain("display: inline-flex");
    expect(rule).toContain("align-items: center");
    expect(rule).toContain("justify-content: center");
    expect(rule).toContain("border-radius: 5px");
    expect(rule).toContain("background: #4435a7");
  });

  it("renders the latest save, summary and game-grouped library", () => {
    render(<SaveManager saves={[makeSave()]} nowMs={nowMs} />);

    expect(screen.getByRole("heading", { name: "我的存档" })).toBeInTheDocument();
    expect(screen.getByText("1 份")).toBeInTheDocument();
    expect(screen.getByText("1 款")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "最近保存" })).toBeInTheDocument();
    expect(screen.getAllByRole("heading", { name: "Metal Slug" })).toHaveLength(2);
    expect(screen.getByText("该游戏目前只有这一份存档")).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "从这里继续" })).toHaveLength(2);
    const sizeLabel = screen.getByText("1.23MB");
    expect(sizeLabel).toHaveAttribute("aria-label", "存档大小 1.23MB");
    expect(sizeLabel.closest(".save-library-shot")).not.toBeNull();
  });

  it("keeps screenshot-less saves resumable and renders stable placeholders", () => {
    render(<SaveManager saves={[makeSave({ screenshotUrl: null })]} nowMs={nowMs} />);

    expect(screen.getAllByRole("img", { name: "Metal Slug 存档画面无预览图" })).toHaveLength(1);
    expect(screen.getByRole("img", { name: "Metal Slug 最近存档画面无预览图" })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "从这里继续" })).toHaveLength(2);
  });

  it("filters locally and keeps the selection in the address bar", async () => {
    const user = userEvent.setup();
    render(<SaveManager saves={[makeSave(), makeSave({ saveStateId: "save-2", gameId: "game-2", gameTitle: "1943", name: "手动存档 2026/8/8" })]} nowMs={nowMs} />);

    const search = screen.getByPlaceholderText("搜索游戏或存档名称");
    await user.type(search, "1943");

    expect(screen.getByRole("region", { name: "筛选存档" })).toHaveTextContent("当前显示 1 份");
    expect(screen.getAllByRole("heading", { name: "1943", level: 2 })).toHaveLength(1);
    expect(screen.queryByRole("heading", { name: "Metal Slug", level: 2 })).not.toBeInTheDocument();
    expect(window.location.search).toBe("?q=1943");
  });

  it("renames and deletes from the compact card menu without refreshing", async () => {
    const user = userEvent.setup();
    render(<SaveManager saves={[makeSave()]} nowMs={nowMs} />);

    await user.click(screen.getByRole("button", { name: "存档“最终关”的更多操作" }));
    await user.click(screen.getByRole("menuitem", { name: "重命名" }));
    const input = screen.getByLabelText("存档名称");
    await user.clear(input);
    await user.type(input, "Boss 前");
    await user.click(screen.getByRole("button", { name: "保存名称" }));

    await waitFor(() => expect(fetch).toHaveBeenCalledWith("/api/v1/saves/save-1", expect.objectContaining({ method: "PATCH", body: JSON.stringify({ name: "Boss 前" }) })));
    expect(screen.getByText("Boss 前")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "存档“Boss 前”的更多操作" }));
    await user.click(screen.getByRole("menuitem", { name: "删除存档" }));
    const dialog = screen.getByRole("alertdialog", { name: "删除这份存档？" });
    await user.click(within(dialog).getByRole("button", { name: "删除存档" }));

    await waitFor(() => expect(fetch).toHaveBeenCalledWith("/api/v1/saves/save-1", expect.objectContaining({ method: "DELETE" })));
    expect(screen.getByText("0 份")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "还没有手动存档" })).toBeInTheDocument();
  });

  it("shows availability details only for blocked saves", () => {
    render(<SaveManager saves={[makeSave({ availability: { status: "BLOCKED", reasons: [{ logicalName: "neogeo.zip" }] } })]} nowMs={nowMs} initialFilters={{ availability: "ALL" }} />);

    expect(screen.getAllByText("当前不可用").length).toBeGreaterThan(0);
    expect(screen.getByRole("alert")).toHaveTextContent("neogeo.zip 当前不可用");
    expect(screen.getByRole("button", { name: "当前不可继续" })).toBeDisabled();
  });

  it("explains that an incompatible runtime save is retained but cannot be restored", () => {
    render(<SaveManager saves={[makeSave({ availability: {
      status: "BLOCKED", reasons: [{ code: "SAVE_RUNTIME_INCOMPATIBLE" }],
    } })]} nowMs={nowMs} initialFilters={{ availability: "ALL" }} />);

    expect(screen.getByRole("alert")).toHaveTextContent("旧版运行时存档已保留，但当前版本无法恢复");
    expect(screen.getByRole("button", { name: "当前不可继续" })).toBeDisabled();
  });
});
