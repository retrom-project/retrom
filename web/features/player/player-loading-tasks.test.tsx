import {cleanup, render, screen, within} from "@testing-library/react";
import {afterEach, expect, it} from "vitest";
import {PlayerLoading} from "./player-loading";
import type {RuntimeStartupTaskV1} from "./runtime/contract";

const props = {immersive: false, message: "正在准备运行模块…", progress: null, returnTo: "/library", state: "loading" as const};
const content = (progress: RuntimeStartupTaskV1["progress"], state: RuntimeStartupTaskV1["state"] = "RUNNING"): RuntimeStartupTaskV1 =>
  ({id: "content", kind: "GAME_CONTENT", state, progress});

afterEach(cleanup);

it("keeps the heading fixed as real startup stages change", () => {
  const {rerender} = render(<PlayerLoading {...props} tasks={[content(null)]} />);
  expect(screen.getByText("游戏启动中", {selector: "strong"})).toBeVisible();
  rerender(<PlayerLoading {...props} message="核心初始化中" tasks={[{id: "core", kind: "CORE_INITIALIZATION", state: "RUNNING", progress: null}]} />);
  expect(screen.getByText("游戏启动中", {selector: "strong"})).toBeVisible();
  expect(screen.getByText("核心初始化")).toBeVisible();
  expect(screen.queryByText("核心初始化中")).toBeNull();
});

it.each([[0, 0], [9, 0], [10, 1], [72, 7], [99, 9], [100, 10]])("fills %s percent as %s of ten slots without implying stage completion", (percentage, filled) => {
  render(<PlayerLoading {...props} tasks={[content({loadedBytes: percentage, totalBytes: 100})]} />);
  const progress = screen.getByRole("progressbar", {name: "游戏内容进度"});
  expect(progress).toHaveAttribute("aria-valuenow", String(percentage));
  expect(progress.querySelectorAll(".player-startup-cell")).toHaveLength(10);
  expect(progress.querySelectorAll(".is-filled")).toHaveLength(filled);
  expect(progress.textContent).toMatch(/^\[.*\]$/u);
  expect(screen.getByRole("img", {name: "游戏内容进行中"})).toBeVisible();
  expect(screen.queryByRole("img", {name: "游戏内容已完成"})).toBeNull();
});

it("separates completed, measured and indeterminate work in the same three-column list", () => {
  render(<PlayerLoading {...props} tasks={[
    {id: "bios", kind: "BIOS", state: "COMPLETED", progress: null},
    content({loadedBytes: 72, totalBytes: 100}),
    {id: "core", kind: "CORE_INITIALIZATION", state: "RUNNING", progress: null},
  ]} />);
  const rows = screen.getAllByRole("listitem");
  expect(rows).toHaveLength(3);
  expect(within(rows[0]).getByRole("progressbar")).toHaveAttribute("aria-valuenow", "100");
  expect(within(rows[0]).getByRole("img", {name: "BIOS已完成"})).toBeVisible();
  expect(rows[0].querySelectorAll(".is-filled")).toHaveLength(10);
  const unknown = within(rows[2]).getByRole("progressbar", {name: "核心初始化进度"});
  expect(unknown).not.toHaveAttribute("aria-valuenow");
  expect(unknown).toHaveAttribute("aria-valuetext", "进度未知，正在处理");
  expect(unknown).toHaveClass("is-indeterminate");
  expect(unknown.querySelectorAll(".is-filled")).toHaveLength(0);
  expect(rows[2].querySelector("[title='核心初始化']")).not.toBeNull();
});

it("freezes failed progress and announces failure without spinning or filling unknown work", () => {
  const {container} = render(<PlayerLoading {...props} state="error" message="启动资源不可用" tasks={[content({loadedBytes: 72, totalBytes: 100})]} />);
  expect(screen.getByText("游戏启动失败", {selector: "strong"})).toBeVisible();
  expect(screen.getByText("启动资源不可用")).toBeVisible();
  expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "72");
  expect(screen.getByRole("img", {name: "游戏内容失败"})).toBeVisible();
  expect(container.querySelector(".player-startup-spinner, .is-indeterminate")).toBeNull();
});

it("retains the last aggregate progress when a module without task events fails", () => {
  render(<PlayerLoading {...props} state="error" message="加载失败" progress={{loadedBytes: 72, totalBytes: 100}} />);
  expect(screen.getByRole("progressbar", {name: "游戏内容进度"})).toHaveAttribute("aria-valuenow", "72");
  expect(screen.getByRole("img", {name: "游戏内容失败"})).toBeVisible();
  expect(screen.queryByRole("img", {name: "游戏内容进行中"})).toBeNull();
});
