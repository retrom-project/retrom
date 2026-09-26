import {act, fireEvent, render, waitFor, within} from "@testing-library/react";
import {afterEach, describe, expect, it, vi} from "vitest";
import type {RuntimeGameEditEntryV1, RuntimeGameEditorV1} from "./runtime/contract";
import {GameEditorPanel} from "./game-editor-panel";

describe("GameEditorPanel", () => {
  afterEach(() => vi.unstubAllGlobals());
  it("shows a draggable category scrollbar only when the category row overflows", async () => {
    let notify: ResizeObserverCallback | undefined;
    vi.stubGlobal("ResizeObserver", class {
      constructor(callback: ResizeObserverCallback) {notify = callback;}
      observe() {}
      disconnect() {}
    });
    const editor: RuntimeGameEditorV1 = {
      categories: async () => [{id: "gold", label: "金币"}, {id: "items", label: "道具"}],
      entries: async () => ({entries: [], nextOffset: null}), set: vi.fn(),
    };
    const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
    await waitFor(() => expect(within(view.container).getByRole("button", {name: "道具"})).toBeVisible());
    const nav = view.container.querySelector<HTMLElement>(".game-editor-categories")!;
    expect(view.container.querySelector(".game-editor-category-scrollbar")).toBeNull();
    Object.defineProperty(nav, "clientWidth", {configurable: true, value: 300});
    Object.defineProperty(nav, "scrollWidth", {configurable: true, value: 800});
    act(() => notify?.([], {} as ResizeObserver));
    const rail = view.container.querySelector<HTMLDivElement>(".game-editor-category-scrollbar")!;
    expect(rail).toBeInTheDocument();
    Object.defineProperty(rail, "clientWidth", {value: 300});
    vi.spyOn(rail, "getBoundingClientRect").mockReturnValue({left: 0} as DOMRect);
    rail.setPointerCapture = vi.fn();
    fireEvent.pointerDown(rail, {button: 0, pointerId: 1, clientX: 275});
    expect(nav.scrollLeft).toBeGreaterThan(0);
    nav.scrollLeft = 0;
    fireEvent.wheel(rail, {deltaY: 120});
    expect(nav.scrollLeft).toBe(120);
    fireEvent.wheel(nav, {deltaY: 500});
    expect(nav.scrollLeft).toBe(500);
    Object.defineProperty(nav, "clientWidth", {configurable: true, value: 800});
    act(() => notify?.([], {} as ResizeObserver));
    expect(view.container.querySelector(".game-editor-category-scrollbar")).toBeNull();
  });
  it("lists map events directly and toggles each event's own A-D switches", async () => {
    const maps = [{id: 1, label: "村庄"}, {id: 2, label: "森林"}];
    const switches = new Map<string, boolean>();
    const events = vi.fn(async (mapId: number) => ({events: [{id: mapId, label: mapId === 1 ? "宝箱" : "大门",
      x: 4, y: 5, switches: Object.fromEntries(["A", "B", "C", "D"].map((key) =>
        [key, switches.get(`${mapId}:${key}`) ?? false])) as Record<"A" | "B" | "C" | "D", boolean>,
      pageUses: [{key: mapId === 1 ? "A" as const : "D" as const, page: 2,
        summary: "无图像、无事件指令"}]}], nextOffset: null}));
    const setSwitch = vi.fn(async (mapId: number, _eventId: number, key: "A" | "B" | "C" | "D", value: boolean) => {
      switches.set(`${mapId}:${key}`, value);
      return (await events(mapId)).events[0];
    });
    const editor: RuntimeGameEditorV1 = {
      categories: async () => [{id: "gold", label: "金币"}, {id: "self_switches", label: "事件独立开关"}],
      entries: async () => ({entries: [], nextOffset: null}), set: vi.fn(),
      selfSwitches: {maps: async (query) => ({currentMapId: 1, currentMapName: "村庄",
        maps: maps.filter((map) => map.label.includes(query)), nextOffset: null}), events, set: setSwitch},
    };
    const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
    const panel = within(view.container);
    fireEvent.click(await panel.findByRole("button", {name: "事件独立开关"}));
    const chest = await panel.findByRole("button", {name: /宝箱 独立开关 A，当前关闭/u});
    expect(panel.getByText("事件 #1 · 坐标 4, 5")).toBeVisible();
    expect(panel.getByText("第 2 页 · 此开关条件未满足 · 无图像、无事件指令")).toBeVisible();
    expect(panel.queryByRole("button", {name: /宝箱 独立开关 B/u})).toBeNull();
    fireEvent.click(chest);
    await waitFor(() => expect(setSwitch).toHaveBeenCalledWith(1, 1, "A", true));
    await panel.findByRole("button", {name: /宝箱 独立开关 A，当前开启/u});
    expect(panel.getByText("第 2 页 · 此开关条件已满足 · 无图像、无事件指令")).toBeVisible();
    fireEvent.click(panel.getByRole("button", {name: /村庄 · 当前地图/u}));
    fireEvent.change(panel.getByRole("searchbox", {name: "查找地图"}), {target: {value: "森林"}});
    fireEvent.click(await panel.findByRole("button", {name: "森林 · 地图 #2"}));
    await panel.findByRole("button", {name: /大门 独立开关 D，当前关闭/u});
    expect(events).toHaveBeenCalledWith(2, "", 0, 40);
    expect(panel.queryByText("宝箱")).toBeNull();
  });
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

  it("edits states and chooses one class within a person's tabs", async () => {
    let poisoned = false;
    let classId = 1;
    const entries = vi.fn(async (category: string) => ({entries: category === "states:1"
      ? [{id: "1:2", label: "中毒", value: poisoned, valueType: "boolean" as const}]
      : category === "states:2"
        ? [{id: "2:2", label: "中毒", value: false, valueType: "boolean" as const}]
        : [{id: "1:1", label: "战士", value: classId === 1, valueType: "boolean" as const},
          {id: "1:2", label: "法师", value: classId === 2, valueType: "boolean" as const}], nextOffset: null}));
    const set = vi.fn(async (category: string, id: string, value: number | string | boolean) => {
      if (category === "states:1") {poisoned = Boolean(value);}
      else {classId = Number(id.split(":")[1]);}
      return {id, label: category === "states:1" ? "中毒" : "法师", value,
        valueType: "boolean" as const};
    });
    const editor: RuntimeGameEditorV1 = {categories: async () => [
      {id: "states", label: "状态", groups: [{id: "states:1", label: "Hero"}, {id: "states:2", label: "Mage"}]},
      {id: "classes", label: "职业", groups: [{id: "classes:1", label: "Hero"}]},
    ], entries, set};
    const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
    const panel = within(view.container);
    await waitFor(() => expect(panel.getByText("当前：未生效")).toBeVisible());
    fireEvent.click(panel.getByRole("button", {name: /中毒.*点击添加/u}));
    await waitFor(() => expect(set).toHaveBeenCalledWith("states:1", "1:2", true));
    expect(panel.getByText("当前：生效中")).toBeVisible();
    fireEvent.click(panel.getByRole("tab", {name: "Mage"}));
    await waitFor(() => expect(entries).toHaveBeenCalledWith("states:2", "", 0, 40));
    fireEvent.click(panel.getByRole("button", {name: "职业"}));
    await waitFor(() => expect(panel.getByText("当前：当前职业")).toBeVisible());
    expect(panel.getByRole("button", {name: /战士.*当前职业/u})).toBeDisabled();
    fireEvent.click(panel.getByRole("button", {name: /法师.*点击转职/u}));
    await waitFor(() => expect(set).toHaveBeenCalledWith("classes:1", "1:2", true));
    await waitFor(() => expect(panel.getByText("当前：当前职业")).toBeVisible());
    expect(panel.getByRole("button", {name: /法师.*当前职业/u})).toBeDisabled();
  });

  it("adds, reorders, and removes party members with direct actions", async () => {
    let party = [1, 2];
    const names = ["Hero", "Mage", "Reserve"];
    const categories = vi.fn(async () => [{id: "party", label: "队伍成员"},
      {id: "actors", label: "角色", groups: party.map((id) => ({id: `actors:${id}`, label: names[id - 1]}))}]);
    const entries = vi.fn(async () => ({entries: names.map((label, index) => ({
      id: String(index + 1), label, value: party.indexOf(index + 1) + 1,
      valueType: "number" as const, min: 0, max: party.includes(index + 1) ? party.length : party.length + 1,
    })), nextOffset: null}));
    const set = vi.fn(async (_category: string, id: string, value: number | string | boolean) => {
      const actorId = Number(id), position = party.indexOf(actorId);
      if (position < 0) {party.push(actorId);}
      else if (value === 0) {party = party.filter((candidate) => candidate !== actorId);}
      else {[party[position], party[Number(value) - 1]] = [party[Number(value) - 1], party[position]];}
      return {id, label: names[actorId - 1], value, valueType: "number" as const, min: 0, max: party.length};
    });
    const editor: RuntimeGameEditorV1 = {categories, entries, set};
    const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
    const panel = within(view.container);
    await waitFor(() => expect(panel.getByText("当前：未入队")).toBeVisible());
    expect(panel.queryByRole("spinbutton")).toBeNull();
    fireEvent.click(panel.getByRole("button", {name: "Reserve加入队伍"}));
    await waitFor(() => expect(set).toHaveBeenCalledWith("party", "3", 3));
    await waitFor(() => expect(panel.getByText("当前：队伍第 3 位")).toBeVisible());
    fireEvent.click(panel.getByRole("button", {name: "Reserve上移"}));
    await waitFor(() => expect(set).toHaveBeenCalledWith("party", "3", 2));
    await waitFor(() => expect(panel.getByText("当前：队伍第 2 位")).toBeVisible());
    fireEvent.click(panel.getByRole("button", {name: "Reserve移出队伍"}));
    await waitFor(() => expect(set).toHaveBeenCalledWith("party", "3", 0));
    expect(categories).toHaveBeenCalledTimes(4);
  });
});
