import {cleanup, fireEvent, render, screen, waitFor, within} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import type * as ApiClient from "@/lib/api/client";
import {ContentEntrySelector} from "./content-entry-selector";

const get = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/client", async original => ({...await original<typeof ApiClient>(), api: {GET: get}}));
afterEach(() => {cleanup(); get.mockReset();});

it("pages runtime KiriKiri candidates directly without opening a second file browser", async () => {
  get.mockImplementation(async (_path, options) => ({response: new Response(), data: options.params.query.prefix
    ? {entries: ["scripts/startup.tjs"], nextCursor: null}
    : options.params.query.after ? {entries: ["patch.xp3"], nextCursor: null}
      : {entries: ["data.xp3", "startup.tjs", "localized.exe"], nextCursor: "localized.exe"}}));
  const onSelect = vi.fn();
  render(<ContentEntrySelector gameId="game" kind="KIRIKIRI_PROJECT" value="data.xp3" onSelect={onSelect} />);
  const select = screen.getByRole("combobox", {name: "入口文件"});
  await waitFor(() => expect(within(select).getByRole("option", {name: "localized.exe"})).toBeInTheDocument());
  expect(get).toHaveBeenLastCalledWith("/api/v1/admin/games/{gameId}/runtime-options/kirikiri", expect.objectContaining({
    params: {path: {gameId: "game"}, query: {after: "", prefix: "", limit: 100}},
  }));
  expect(screen.queryByText("浏览项目文件")).not.toBeInTheDocument();
  expect(screen.getByRole("button", {name: "返回第一页"})).toBeDisabled();
  fireEvent.click(screen.getByRole("button", {name: "下一页入口"}));
  await waitFor(() => expect(within(select).getByRole("option", {name: "patch.xp3"})).toBeInTheDocument());
  expect(select).toHaveValue("data.xp3");
  expect(screen.getByRole("button", {name: "下一页入口"})).toBeDisabled();
  fireEvent.click(screen.getByRole("button", {name: "返回第一页"}));
  await waitFor(() => expect(within(select).getByRole("option", {name: "localized.exe"})).toBeInTheDocument());
  fireEvent.click(screen.getByRole("button", {name: "下一页入口"}));
  await waitFor(() => expect(within(select).getByRole("option", {name: "patch.xp3"})).toBeInTheDocument());
  fireEvent.change(screen.getByRole("textbox", {name: "按路径筛选"}), {target: {value: "scripts/"}});
  await waitFor(() => expect(within(select).getByRole("option", {name: "scripts/startup.tjs"})).toBeInTheDocument());
  expect(get).toHaveBeenLastCalledWith(expect.any(String), expect.objectContaining({
    params: {path: {gameId: "game"}, query: {after: "", prefix: "scripts/", limit: 100}},
  }));
  expect(onSelect).not.toHaveBeenCalled();expect(select).toHaveValue("data.xp3");
  fireEvent.change(select, {target: {value: "scripts/startup.tjs"}});
  expect(onSelect).toHaveBeenCalledExactlyOnceWith("scripts/startup.tjs");
});

it("keeps other project content paging on its existing API", async () => {
  get.mockResolvedValue({response: new Response(), data: {items: [{logicalKey: "0.txt"}], nextCursor: null}});
  render(<ContentEntrySelector gameId="game" kind="ONS_PROJECT" value="0.txt" onSelect={vi.fn()} />);
  await waitFor(() => expect(screen.getByRole("combobox", {name: "入口文件"})).toBeEnabled());
  expect(screen.getByRole("option", {name: "0.txt"})).toBeInTheDocument();
  expect(get).toHaveBeenCalledWith("/api/v1/admin/games/{gameId}/content", expect.anything());
});

it("distinguishes empty filters from errors and permits retry without changing the entry", async () => {
  let failing = true;
  get.mockImplementation(async (_path, options) => {
    if (options.params.query.prefix === "scripts/" && failing) {throw new Error("入口读取失败");}
    return {response: new Response(), data: {entries: [], nextCursor: null}};
  });
  render(<ContentEntrySelector gameId="game" kind="KIRIKIRI_PROJECT" value="startup.tjs" onSelect={vi.fn()} />);
  expect(await screen.findByText("没有匹配的入口候选。")).toBeVisible();
  expect(screen.getByRole("button", {name: "下一页入口"})).toBeDisabled();
  fireEvent.change(screen.getByRole("textbox", {name: "按路径筛选"}), {target: {value: "scripts/"}});
  expect(await screen.findByRole("alert")).toHaveTextContent("入口读取失败");
  expect(screen.queryByText("没有匹配的入口候选。")).not.toBeInTheDocument();
  failing = false;fireEvent.click(screen.getByRole("button", {name: "重试"}));
  expect(await screen.findByText("没有匹配的入口候选。")).toBeVisible();
  expect(screen.getByRole("combobox", {name: "入口文件"})).toHaveValue("startup.tjs");
});
