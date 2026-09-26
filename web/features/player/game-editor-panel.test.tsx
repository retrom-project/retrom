import {fireEvent, render, waitFor, within} from "@testing-library/react";
import {describe, expect, it, vi} from "vitest";
import type {RuntimeGameEditorV1} from "./runtime/contract";
import {GameEditorPanel} from "./game-editor-panel";

describe("GameEditorPanel", () => {
  it("lists named values directly and writes them without a search or an ID", async () => {
    const set = vi.fn(async (_category: string, id: string, value: number | string | boolean) =>
      ({id, label: "金币", value, valueType: "number" as const, min: 0, max: 100}));
    const entries = vi.fn(async (category: string) => ({entries: category === "gold"
      ? [{id: "gold", label: "金币", value: 10, valueType: "number" as const, min: 0, max: 100}]
      : [{id: "1", label: "魔法药", value: 0, valueType: "number" as const, min: 0, max: 99}], nextOffset: null}));
    const editor: RuntimeGameEditorV1 = {categories: async () => [{id: "gold", label: "金币"},
      {id: "items", label: "道具"}], entries, set};
    const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
    const panel = within(view.container);
    await waitFor(() => expect(panel.getByText("当前：10", {exact: false})).toBeVisible());
    expect(panel.queryByRole("searchbox")).toBeNull();
    expect(panel.queryByText("#gold", {exact: false})).toBeNull();
    expect(entries).toHaveBeenCalledWith("gold", "", 0, 40);
    const close = panel.getByRole("button", {name: "返回游戏"});
    const focusable = view.container.querySelectorAll("button:not(:disabled), input:not(:disabled)");
    close.focus();
    fireEvent.keyDown(close, {key: "Tab", shiftKey: true});
    expect(focusable.item(focusable.length - 1)).toHaveFocus();
    fireEvent.change(panel.getByRole("spinbutton", {name: "修改金币"}), {target: {value: "50"}});
    fireEvent.click(panel.getByRole("button", {name: "应用"}));
    await waitFor(() => expect(set).toHaveBeenCalledWith("gold", "gold", 50));
    fireEvent.click(panel.getByRole("button", {name: "道具"}));
    await waitFor(() => expect(panel.getByText("魔法药")).toBeVisible());
    expect(panel.getByText("当前：0", {exact: false})).toBeVisible();
    expect(panel.queryByRole("searchbox")).toBeNull();
    expect(panel.getByRole("dialog")).toHaveTextContent("离开前请创建存档");
  });

  it("offers optional lookup and more rows for long lists", async () => {
    const entries = vi.fn(async (_category: string, _query: string, offset: number) => ({
      entries: [{id: String(offset + 1), label: `护甲 ${offset + 1}`, value: 0,
        valueType: "number" as const, min: 0, max: 99}], nextOffset: offset === 0 ? 40 : null,
    }));
    const editor: RuntimeGameEditorV1 = {categories: async () => [{id: "armors", label: "护甲"}],
      entries, set: vi.fn()};
    const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
    const panel = within(view.container);
    await waitFor(() => expect(panel.getByText("护甲 1")).toBeVisible());
    fireEvent.click(panel.getByRole("button", {name: "显示更多"}));
    await waitFor(() => expect(panel.getByText("护甲 41")).toBeVisible());
    fireEvent.click(panel.getByRole("button", {name: /^查找$/u}));
    fireEvent.change(panel.getByRole("searchbox", {name: "按名称查找"}), {target: {value: "护甲"}});
    fireEvent.click(panel.getByRole("button", {name: /^查找$/u}));
    await waitFor(() => expect(entries).toHaveBeenCalledWith("armors", "护甲", 0, 40));
  });
});
