import {act, cleanup, fireEvent, render, waitFor, within} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import type {RuntimeGameEditEntryV1, RuntimeGameEditorV1} from "./runtime/contract";
import {GameEditorPanel} from "./game-editor-panel";

afterEach(async () => {await act(async () => cleanup()); vi.restoreAllMocks(); vi.unstubAllGlobals();});

it("opens nested values, edits decimals and returns to the containing variable", async () => {
  let score = 2.5;
  const collection: RuntimeGameEditEntryV1 = {id: "1", label: "角色属性", value: "数组 · 1 项",
    valueType: "collection", children: "variables:1"};
  const nested: RuntimeGameEditEntryV1 = {id: "1:1", label: "[0]", value: "对象 · 2 项",
    valueType: "collection", children: "variables:1:1"};
  const number = (): RuntimeGameEditEntryV1 => ({id: "1:2", label: "score", value: score,
    valueType: "number", min: -Number.MAX_SAFE_INTEGER, max: Number.MAX_SAFE_INTEGER, step: "any"});
  const editor: RuntimeGameEditorV1 = {
    categories: async () => [{id: "variables", label: "变量"}],
    entries: vi.fn<RuntimeGameEditorV1["entries"]>(async (category) => ({entries: category === "variables" ? [collection]
      : category === "variables:1" ? [nested] : [number(), {id: "1:3", label: "optional", value: null, valueType: "null"}], nextOffset: null})),
    set: vi.fn(async (_category, _id, value) => {score = Number(value); return number();}),
  };
  const view = render(<GameEditorPanel editor={editor} native onClose={vi.fn()} />);
  const panel = within(view.container);
  fireEvent.click(await panel.findByRole("button", {name: "展开角色属性"}));
  fireEvent.click(await panel.findByRole("button", {name: "展开[0]"}));
  expect(panel.getByRole("navigation", {name: "数值路径"})).toHaveTextContent("变量 / 角色属性 / [0]");
  const input = await panel.findByRole("spinbutton", {name: "修改score"});
  expect(input).toHaveAttribute("step", "any");
  expect(input).toHaveAttribute("inputmode", "decimal");
  expect(panel.getByText("当前：空值")).toBeVisible();
  expect(panel.queryByLabelText("修改optional")).toBeNull();
  fireEvent.change(input, {target: {value: "3.75"}});
  fireEvent.click(panel.getByRole("button", {name: "应用"}));
  await waitFor(() => expect(editor.set).toHaveBeenCalledWith("variables:1:1", "1:2", 3.75));
  expect(await panel.findByRole("status")).toHaveTextContent("已应用修改");
  fireEvent.click(panel.getByRole("button", {name: "返回上一级"}));
  await panel.findByRole("button", {name: "展开[0]"});
  fireEvent.click(panel.getByRole("button", {name: "返回上一级"}));
  await panel.findByRole("button", {name: "展开角色属性"});
  expect(panel.queryByRole("navigation", {name: "数值路径"})).toBeNull();
});

it("ignores a delayed nested response after the user returns and preserves integer controls", async () => {
  let complete!: (page: {entries: RuntimeGameEditEntryV1[]; nextOffset: null}) => void;
  const collection: RuntimeGameEditEntryV1 = {id: "1", label: "统计", value: "对象 · 1 项",
    valueType: "collection", children: "variables:1"};
  const editor: RuntimeGameEditorV1 = {
    categories: async () => [{id: "variables", label: "变量"}, {id: "gold", label: "金币"}],
    entries: vi.fn<RuntimeGameEditorV1["entries"]>(async (category) => category === "variables:1"
      ? new Promise(resolve => {complete = resolve;})
      : {entries: category === "variables" ? [collection]
        : [{id: "gold", label: "金币", value: 10, valueType: "number", min: 0}], nextOffset: null}),
    set: vi.fn(),
  };
  const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
  const panel = within(view.container);
  fireEvent.click(await panel.findByRole("button", {name: "展开统计"}));
  await waitFor(() => expect(complete).toBeTypeOf("function"));
  fireEvent.click(panel.getByRole("button", {name: "返回上一级"}));
  await panel.findByRole("button", {name: "展开统计"});
  await act(async () => complete({entries: [{id: "late", label: "过期数值", value: 5, valueType: "number"}], nextOffset: null}));
  expect(panel.queryByText("过期数值")).toBeNull();
  fireEvent.click(panel.getByRole("button", {name: "金币"}));
  const gold = await panel.findByRole("spinbutton", {name: "修改金币"});
  expect(gold).toHaveAttribute("step", "1");
  expect(gold).toHaveAttribute("inputmode", "numeric");
});

it("restores the parent's search and focuses the expanded entry on return", async () => {
  const collection: RuntimeGameEditEntryV1 = {id: "87", label: "角色属性", value: "数组 · 1 项",
    valueType: "collection", children: "variables:87"};
  const editor: RuntimeGameEditorV1 = {
    categories: async () => [{id: "variables", label: "变量"}], set: vi.fn(),
    entries: vi.fn<RuntimeGameEditorV1["entries"]>(async (category, query) => ({
      entries: category === "variables" && query === "角色" ? [collection] : [], nextOffset: null,
    })),
  };
  const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
  const panel = within(view.container);
  fireEvent.click(await panel.findByRole("button", {name: "查找"}));
  const query = panel.getByRole("searchbox", {name: "按名称查找"});
  fireEvent.change(query, {target: {value: "角色"}});
  fireEvent.submit(query.closest("form")!);
  fireEvent.click(await panel.findByRole("button", {name: "展开角色属性"}));
  fireEvent.click(panel.getByRole("button", {name: "返回上一级"}));
  const entry = await panel.findByRole("button", {name: "展开角色属性"});
  expect(panel.getByRole("searchbox", {name: "按名称查找"})).toHaveValue("角色");
  await waitFor(() => expect(entry).toHaveFocus());
});

it("restores a later parent page before focusing its expanded collection", async () => {
  let intersection!: IntersectionObserverCallback;
  vi.stubGlobal("IntersectionObserver", class {
    constructor(callback: IntersectionObserverCallback) {intersection = callback;}
    observe() {}
    disconnect() {}
  });
  const target: RuntimeGameEditEntryV1 = {id: "87", label: "末项", value: "数组 · 0 项",
    valueType: "collection", children: "variables:87"};
  const editor: RuntimeGameEditorV1 = {
    categories: async () => [{id: "variables", label: "变量"}], set: vi.fn(),
    entries: vi.fn<RuntimeGameEditorV1["entries"]>(async (category, _query, offset) => category !== "variables"
      ? {entries: [], nextOffset: null} : offset === 0
        ? {entries: Array.from({length: 40}, (_, index) => ({id: String(index + 1), label: `值 ${index}`,
          value: index, valueType: "number"})), nextOffset: 40}
        : {entries: [target], nextOffset: null}),
  };
  const view = render(<GameEditorPanel editor={editor} onClose={vi.fn()} />);
  const panel = within(view.container);
  await waitFor(() => expect(intersection).toBeTypeOf("function"));
  act(() => intersection([{isIntersecting: true} as IntersectionObserverEntry], {} as IntersectionObserver));
  fireEvent.click(await panel.findByRole("button", {name: "展开末项"}));
  fireEvent.click(panel.getByRole("button", {name: "返回上一级"}));
  await waitFor(() => expect(panel.getByRole("button", {name: "展开末项"})).toHaveFocus());
  expect(editor.entries).toHaveBeenLastCalledWith("variables", "", 40, 40);
});
