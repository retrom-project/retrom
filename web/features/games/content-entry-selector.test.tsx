import {cleanup, fireEvent, render, screen, waitFor, within} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import type * as ApiClient from "@/lib/api/client";
import {ContentEntrySelector} from "./content-entry-selector";

const get = vi.hoisted(() => vi.fn());
vi.mock("@/lib/api/client", async original => ({...await original<typeof ApiClient>(), api: {GET: get}}));
afterEach(() => {cleanup(); get.mockReset();});

it("keeps the selected logical entry and reads bounded pages instead of the physical ZIP", async () => {
  get.mockImplementation(async (_path, options) => ({response: new Response(), data: {
    items: [{logicalKey: options.params.query.after || options.params.query.prefix ? "scripts/startup.tjs" : "data/0001.xp3", sizeBytes: 1, sha256: "hash", mediaType: "application/octet-stream"}],
    nextCursor: options.params.query.after || options.params.query.prefix ? null : "data/0001.xp3",
  }}));
  const onSelect = vi.fn();
  render(<ContentEntrySelector gameId="game" value="startup.tjs" onSelect={onSelect} />);
  const select = await screen.findByRole("combobox", {name: "入口文件"});
  await waitFor(() => expect(within(select).getByRole("option", {name: "data/0001.xp3"})).toBeInTheDocument());
  expect(select).toHaveValue("startup.tjs");
  expect(screen.queryByRole("option", {name: "game.zip"})).not.toBeInTheDocument();
  fireEvent.click(screen.getByText("浏览项目文件"));
  const list = screen.getByRole("list", {name: "项目文件"});
  expect(within(list).getByRole("button", {name: /data\/0001\.xp3/})).toBeVisible();
  expect(screen.getByRole("button", {name: "返回第一页"})).toBeDisabled();
  fireEvent.click(screen.getByRole("button", {name: "下一页文件"}));
  await waitFor(() => expect(get).toHaveBeenLastCalledWith("/api/v1/admin/games/{gameId}/content", expect.objectContaining({
    params: {path: {gameId: "game"}, query: {after: "data/0001.xp3", prefix: "", limit: 100}},
  })));
  await waitFor(() => expect(within(screen.getByRole("list", {name: "项目文件"})).getByRole("button", {name: /scripts\/startup\.tjs/})).toBeVisible());
  expect(screen.queryByRole("button", {name: /data\/0001\.xp3/})).not.toBeInTheDocument();
  expect(screen.getByRole("button", {name: "下一页文件"})).toBeDisabled();
  fireEvent.click(screen.getByRole("button", {name: "返回第一页"}));
  await waitFor(() => expect(within(screen.getByRole("list", {name: "项目文件"})).getByRole("button", {name: /data\/0001\.xp3/})).toBeVisible());
  fireEvent.change(screen.getByRole("textbox", {name: "路径前缀"}), {target: {value: "scripts/"}});
  await waitFor(() => expect(get).toHaveBeenLastCalledWith(expect.any(String), expect.objectContaining({
    params: {path: {gameId: "game"}, query: {after: "", prefix: "scripts/", limit: 100}},
  })));
  await waitFor(() => expect(within(screen.getByRole("list", {name: "项目文件"})).getByRole("button", {name: /scripts\/startup\.tjs/})).toBeVisible());
  expect(onSelect).not.toHaveBeenCalled();
  fireEvent.click(within(screen.getByRole("list", {name: "项目文件"})).getByRole("button", {name: /scripts\/startup\.tjs/}));
  expect(onSelect).toHaveBeenCalledExactlyOnceWith("scripts/startup.tjs");
});

it("distinguishes an empty filter from a read failure and permits retry", async () => {
  let failing = true;
  get.mockImplementation(async (_path, options) => {
    if (options.params.query.prefix === "scripts/" && failing) {throw new Error("文件读取失败");}
    return {response: new Response(), data: {items: [], nextCursor: null}};
  });
  render(<ContentEntrySelector gameId="game" value="startup.tjs" onSelect={vi.fn()} />);
  fireEvent.click(screen.getByText("浏览项目文件"));
  expect(await screen.findByText("没有匹配的项目文件。")).toBeVisible();
  expect(screen.getByRole("button", {name: "下一页文件"})).toBeDisabled();
  fireEvent.change(screen.getByRole("textbox", {name: "路径前缀"}), {target: {value: "scripts/"}});
  expect(await screen.findByRole("alert")).toHaveTextContent("文件读取失败");
  expect(screen.queryByText("没有匹配的项目文件。")).not.toBeInTheDocument();
  failing = false;
  fireEvent.click(screen.getByRole("button", {name: "重试"}));
  expect(await screen.findByText("没有匹配的项目文件。")).toBeVisible();
  expect(screen.getByRole("combobox", {name: "入口文件"})).toHaveValue("startup.tjs");
});
