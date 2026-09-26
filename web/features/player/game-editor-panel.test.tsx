import {fireEvent, render, waitFor, within} from "@testing-library/react";
import {afterEach, describe, expect, it, vi} from "vitest";
import type {RuntimeGameEditEntryV1, RuntimeGameEditorV1} from "./runtime/contract";
import {GameEditorPanel} from "./game-editor-panel";

describe("GameEditorPanel", () => {
  afterEach(() => vi.unstubAllGlobals());
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
    const notice = await panel.findByRole("status");
    expect(notice).toHaveTextContent("已应用修改");
    expect(notice.parentElement).toContainElement(view.container.querySelector(".game-editor-help"));
    fireEvent.click(panel.getByRole("button", {name: "道具"}));
    await waitFor(() => expect(panel.getByText("魔法药")).toBeVisible());
    expect(panel.getByText("当前：0", {exact: false})).toBeVisible();
    expect(panel.queryByRole("searchbox")).toBeNull();
    expect(panel.getByRole("dialog")).toHaveTextContent("离开前请创建存档");
  });

  it("loads more rows when the list end enters view, without duplicate requests", async () => {
    let onIntersect: IntersectionObserverCallback | undefined;
    let observed: Element | undefined;
    let root: Element | Document | null | undefined;
    vi.stubGlobal("IntersectionObserver", class {
      constructor(callback: IntersectionObserverCallback, options?: IntersectionObserverInit) {
        onIntersect = callback;
        root = options?.root;
      }
      observe(target: Element) {observed = target;}
      disconnect() {}
    });
    let finishPage: ((value: {entries: {id: string; label: string; value: number; valueType: "number"; min: number; max: number}[]; nextOffset: null}) => void) | undefined;
    const entries = vi.fn(async (_category: string, _query: string, offset: number) => offset === 40
      ? new Promise<{entries: {id: string; label: string; value: number; valueType: "number"; min: number; max: number}[]; nextOffset: null}>((resolve) => {finishPage = resolve;})
      : ({
      entries: [{id: String(offset + 1), label: `护甲 ${offset + 1}`, value: 0,
        valueType: "number" as const, min: 0, max: 99}], nextOffset: 40,
    }));
    const editor: RuntimeGameEditorV1 = {categories: async () => [{id: "armors", label: "护甲"}],
      entries, set: vi.fn()};
    const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
    const panel = within(view.container);
    await waitFor(() => expect(panel.getByText("护甲 1")).toBeVisible());
    await waitFor(() => expect(observed).toBeTruthy());
    expect(root).toBe(view.container.querySelector(".game-editor-list"));
    expect(panel.queryByRole("button", {name: "显示更多"})).toBeNull();
    onIntersect?.([{isIntersecting: true, target: observed} as IntersectionObserverEntry], {} as IntersectionObserver);
    onIntersect?.([{isIntersecting: true, target: observed} as IntersectionObserverEntry], {} as IntersectionObserver);
    expect(entries).toHaveBeenCalledTimes(2);
    finishPage?.({entries: [{id: "41", label: "护甲 41", value: 0,
      valueType: "number", min: 0, max: 99}], nextOffset: null});
    await waitFor(() => expect(panel.getByText("护甲 41")).toBeVisible());
    fireEvent.click(panel.getByRole("button", {name: /^查找$/u}));
    fireEvent.change(panel.getByRole("searchbox", {name: "按名称查找"}), {target: {value: "护甲"}});
    fireEvent.click(panel.getByRole("button", {name: /^查找$/u}));
    await waitFor(() => expect(entries).toHaveBeenCalledWith("armors", "护甲", 0, 40));
    fireEvent.click(panel.getByRole("button", {name: "清除"}));
    await waitFor(() => expect(entries).toHaveBeenLastCalledWith("armors", "", 0, 40));
  });

  it("keeps existing rows and offers retry when the next page fails", async () => {
    let onIntersect: IntersectionObserverCallback | undefined;
    let observed: Element | undefined;
    vi.stubGlobal("IntersectionObserver", class {
      constructor(callback: IntersectionObserverCallback) {onIntersect = callback;}
      observe(target: Element) {observed = target;}
      disconnect() {}
    });
    let pageAttempts = 0;
    const entries = vi.fn(async (_category: string, _query: string, offset: number) => {
      if (offset === 40 && pageAttempts++ === 0) {throw new Error("read failed");}
      return {entries: [{id: String(offset + 1), label: `护甲 ${offset + 1}`, value: 0,
        valueType: "number" as const, min: 0, max: 99}], nextOffset: offset === 0 ? 40 : null};
    });
    const editor: RuntimeGameEditorV1 = {categories: async () => [{id: "armors", label: "护甲"}],
      entries, set: vi.fn()};
    const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
    const panel = within(view.container);
    await waitFor(() => expect(observed).toBeTruthy());
    onIntersect?.([{isIntersecting: true, target: observed} as IntersectionObserverEntry], {} as IntersectionObserver);
    await waitFor(() => expect(panel.getByRole("button", {name: "重试"})).toBeVisible());
    expect(panel.getByText("护甲 1")).toBeVisible();
    fireEvent.click(panel.getByRole("button", {name: "重试"}));
    await waitFor(() => expect(panel.getByText("护甲 41")).toBeVisible());
    expect(entries).toHaveBeenCalledTimes(3);
  });

  it("shows one actor's skills per tab with direct learn and forget actions", async () => {
    let learned = false;
    const entry = () => ({id: "1:2", label: "Fire", value: learned, valueType: "boolean" as const});
    const set = vi.fn(async (_category: string, _id: string, value: number | string | boolean) => {
      learned = value === true;
      return entry();
    });
    const entries = vi.fn(async (category: string) => ({entries: category === "skills:1" ? [entry()]
      : [{id: "2:3", label: "Cure", value: true, valueType: "boolean" as const}], nextOffset: null}));
    const editor: RuntimeGameEditorV1 = {categories: async () => [{id: "skills", label: "技能",
      groups: [{id: "skills:1", label: "Hero"}, {id: "skills:2", label: "Mage"}]}], entries, set};
    const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
    const panel = within(view.container);
    await waitFor(() => expect(panel.getByText("Fire")).toBeVisible());
    expect(entries).toHaveBeenCalledWith("skills:1", "", 0, 40);
    expect(panel.getByRole("tab", {name: "Hero"})).toHaveAttribute("aria-selected", "true");
    expect(panel.getByText("当前：未学会")).toBeVisible();
    fireEvent.click(panel.getByRole("button", {name: /Fire.*点击学习/u}));
    await waitFor(() => expect(set).toHaveBeenCalledWith("skills:1", "1:2", true));
    expect(panel.getByText("当前：已学会")).toBeVisible();
    fireEvent.click(panel.getByRole("button", {name: /Fire.*点击遗忘/u}));
    await waitFor(() => expect(set).toHaveBeenCalledWith("skills:1", "1:2", false));
    fireEvent.click(panel.getByRole("button", {name: /^查找$/u}));
    const searchInput = view.container.querySelector<HTMLInputElement>('input[type="search"]');
    expect(searchInput).not.toBeNull();
    fireEvent.change(searchInput!, {target: {value: "Fire"}});
    fireEvent.click(panel.getByRole("button", {name: /^查找$/u}));
    await waitFor(() => expect(entries).toHaveBeenCalledWith("skills:1", "Fire", 0, 40));
    const list = view.container.querySelector<HTMLElement>(".game-editor-list")!;
    list.scrollTop = 120;
    fireEvent.click(panel.getByRole("tab", {name: "Mage"}));
    await waitFor(() => expect(panel.getByText("Cure")).toBeVisible());
    expect(panel.queryByText("Fire")).toBeNull();
    expect(view.container.querySelector('input[type="search"]')).toBeNull();
    expect(list.scrollTop).toBe(0);
    expect(entries).toHaveBeenCalledWith("skills:2", "", 0, 40);
    expect(panel.getByRole("tab", {name: "Mage"})).toHaveAttribute("aria-selected", "true");
  });

  it("shows actor attributes under the selected person's tab", async () => {
    let mageHp = 8;
    const entries = vi.fn(async (category: string) => ({entries: category === "actors:1"
      ? [{id: "1:hp", label: "生命", value: 10, valueType: "number" as const, min: 0, max: 20}]
      : [{id: "2:hp", label: "生命", value: mageHp, valueType: "number" as const, min: 0, max: 20}], nextOffset: null}));
    const set = vi.fn(async (_category: string, id: string, value: number | string | boolean) => {
      mageHp = Number(value);
      return {id, label: "生命", value: mageHp, valueType: "number" as const, min: 0, max: 20};
    });
    const editor: RuntimeGameEditorV1 = {categories: async () => [{id: "actors", label: "角色",
      groups: [{id: "actors:1", label: "Hero"}, {id: "actors:2", label: "Mage"}]}], entries, set};
    const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
    const panel = within(view.container);
    await waitFor(() => expect(panel.getByText("当前：10")).toBeVisible());
    expect(entries).toHaveBeenCalledWith("actors:1", "", 0, 40);
    fireEvent.click(panel.getByRole("tab", {name: "Mage"}));
    await waitFor(() => expect(panel.getByText("当前：8")).toBeVisible());
    expect(panel.queryByText("当前：10")).toBeNull();
    expect(entries).toHaveBeenCalledWith("actors:2", "", 0, 40);
    fireEvent.change(panel.getByRole("spinbutton", {name: "修改生命"}), {target: {value: "12"}});
    fireEvent.click(panel.getByRole("button", {name: "应用"}));
    await waitFor(() => expect(set).toHaveBeenCalledWith("actors:2", "2:hp", 12));
  });

  it("keeps the list quiet during a quick category transition", async () => {
    let resolveItems: ((value: {entries: RuntimeGameEditEntryV1[]; nextOffset: null}) => void) | undefined;
    const entries = vi.fn(async (category: string) => category === "gold"
      ? {entries: [{id: "gold", label: "金币", value: 10, valueType: "number" as const, min: 0, max: 100}], nextOffset: null}
      : new Promise<{entries: RuntimeGameEditEntryV1[]; nextOffset: null}>((resolve) => {resolveItems = resolve;}));
    const editor: RuntimeGameEditorV1 = {categories: async () => [{id: "gold", label: "金币"}, {id: "items", label: "道具"}],
      entries, set: vi.fn()};
    const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
    const panel = within(view.container);
    await waitFor(() => expect(panel.getByText("当前：10")).toBeVisible());
    fireEvent.click(panel.getByRole("button", {name: "道具"}));
    expect(panel.queryByText("正在读取…")).toBeNull();
    expect(panel.queryByText("当前：10")).toBeNull();
    expect(view.container.querySelector(".game-editor-list")).toHaveAttribute("aria-busy", "true");
    await waitFor(() => expect(entries).toHaveBeenCalledWith("items", "", 0, 40));
    resolveItems?.({entries: [{id: "2", label: "药水", value: 1, valueType: "number", min: 0, max: 99}], nextOffset: null});
    await waitFor(() => expect(panel.getByText("药水")).toBeVisible());
  });
});
