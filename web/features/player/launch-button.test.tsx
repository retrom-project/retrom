import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import type * as ApiClient from "@/lib/api/client";
import { LaunchButton } from "./launch-button";

const calls = vi.hoisted(() => ({ create: vi.fn(), remove: vi.fn() }));
const initialFullscreen = Object.getOwnPropertyDescriptor(document.documentElement, "requestFullscreen");
const initialEnabled = Object.getOwnPropertyDescriptor(document, "fullscreenEnabled");
const initialElement = Object.getOwnPropertyDescriptor(document, "fullscreenElement");
const initialExit = Object.getOwnPropertyDescriptor(document, "exitFullscreen");
vi.mock("next/navigation", () => ({ useRouter: () => ({
  push: (path: string) => window.history.pushState(null, "", path),
  replace: (path: string) => window.history.replaceState(null, "", path),
}) }));
vi.mock("@/lib/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof ApiClient>();
  return { ...actual, api: { POST: calls.create, DELETE: calls.remove } };
});
beforeEach(() => {
  calls.create.mockReset(); calls.create.mockResolvedValue({ data: { id: "new-run" }, response: new Response() });
  calls.remove.mockReset(); calls.remove.mockResolvedValue({ response: new Response(null, { status: 204 }) });
});
afterEach(() => {
  cleanup();
  for (const [target, key, descriptor] of [
    [document.documentElement, "requestFullscreen", initialFullscreen],
    [document, "fullscreenEnabled", initialEnabled],
    [document, "fullscreenElement", initialElement],
    [document, "exitFullscreen", initialExit],
  ] as const) {
    if (descriptor) { Object.defineProperty(target, key, descriptor); }
    else { Reflect.deleteProperty(target, key); }
  }
});

it.each([
  { path: "/games/game?from=favorites#launch", purpose: "play" as const, saveId: undefined, returnTo: undefined },
  { path: "/saves?gameId=game&kind=checkpoint", purpose: "play" as const, saveId: "save", returnTo: undefined },
  { path: "/admin/reviews/game", purpose: "review" as const, saveId: undefined, returnTo: undefined },
  { path: "/immersive", purpose: "play" as const, saveId: "save", returnTo: "/immersive?view=saves&destination=folder&offset=24&folder=mine&entry=save" },
])("replaces the current entry and retains launch/return context from $path", async ({ path, purpose, saveId, returnTo }) => {
  window.history.replaceState(null, "", path);
  const historyLength = window.history.length;
  render(<LaunchButton gameId="game" coreId="core" saveId={saveId} purpose={purpose} returnTo={returnTo} contentLoading="ON_DEMAND" />);
  fireEvent.click(screen.getByRole("button", { name: "开始游戏" }));
  await waitFor(() => expect(window.location.pathname).toBe("/play/new-run"));
  expect(window.history.length).toBe(historyLength);
  expect(new URLSearchParams(window.location.search).get("returnTo")).toBe(returnTo ?? path);
  expect(new URLSearchParams(window.location.search).get("contentLoading")).toBe("ON_DEMAND");
  expect(calls.create).toHaveBeenCalledExactlyOnceWith("/api/v1/runs", { body: { gameId: "game", coreId: "core", saveId, purpose } });
});

it("keeps the origin entry and permits retry if Run creation fails", async () => {
  calls.create.mockRejectedValueOnce(new Error("启动失败"));
  window.history.replaceState(null, "", "/games/game?from=recent");
  const historyLength = window.history.length;
  render(<LaunchButton gameId="game" />);
  fireEvent.click(screen.getByRole("button", { name: "开始游戏" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("启动失败");
  expect(window.location.pathname + window.location.search).toBe("/games/game?from=recent");
  expect(window.history.length).toBe(historyLength);
  expect(screen.getByRole("button", { name: "开始游戏" })).toBeEnabled();
});

it("requests fullscreen from the launch gesture before waiting for Run creation", async () => {
  const fullscreen = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(document.documentElement, "requestFullscreen", {configurable: true, value: fullscreen});
  Object.defineProperty(document, "fullscreenEnabled", {configurable: true, value: true});
  let finish: (value: unknown) => void = () => {};
  calls.create.mockImplementation(() => new Promise(resolve => {finish = resolve;}));
  render(<LaunchButton gameId="game" />);
  fireEvent.click(screen.getByRole("button", {name: "开始游戏"}));
  expect(fullscreen).toHaveBeenCalledOnce();
  expect(fullscreen.mock.invocationCallOrder[0]).toBeLessThan(calls.create.mock.invocationCallOrder[0]);
  finish({data: {id: "new-run"}, response: new Response()});
  await waitFor(() => expect(window.location.pathname).toBe("/play/new-run"));
});

it("continues launching when fullscreen is rejected", async () => {
  Object.defineProperty(document.documentElement, "requestFullscreen", {configurable: true, value: vi.fn().mockRejectedValue(new Error("denied"))});
  Object.defineProperty(document, "fullscreenEnabled", {configurable: true, value: true});
  render(<LaunchButton gameId="game" />);
  fireEvent.click(screen.getByRole("button", {name: "开始游戏"}));
  await waitFor(() => expect(calls.create).toHaveBeenCalledOnce());
  await waitFor(() => expect(window.location.pathname).toBe("/play/new-run"));
});

it.each([false, true])("a failed launch leaves prior fullscreen intact and clears only its own request: active=%s", async active => {
  let fullscreen: Element | null = active ? document.documentElement : null;
  const request = vi.fn().mockImplementation(async () => {fullscreen = document.documentElement;});
  const exit = vi.fn().mockImplementation(async () => {fullscreen = null;});
  Object.defineProperty(document.documentElement, "requestFullscreen", {configurable: true, value: request});
  Object.defineProperty(document, "fullscreenEnabled", {configurable: true, value: true});
  Object.defineProperty(document, "fullscreenElement", {configurable: true, get: () => fullscreen});
  Object.defineProperty(document, "exitFullscreen", {configurable: true, value: exit});
  calls.create.mockRejectedValueOnce(new Error("启动失败"));
  render(<LaunchButton gameId="game" />);
  fireEvent.click(screen.getByRole("button", {name: "开始游戏"}));
  expect(await screen.findByRole("alert")).toHaveTextContent("启动失败");
  expect(request).toHaveBeenCalledTimes(active ? 0 : 1);
  expect(exit).toHaveBeenCalledTimes(active ? 0 : 1);
});


it("warns before creating a Run or requesting fullscreen and permits cancellation", async () => {
  const fullscreen = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(document.documentElement, "requestFullscreen", { configurable: true, value: fullscreen });
  Object.defineProperty(document, "fullscreenEnabled", { configurable: true, value: true });
  render(<LaunchButton gameId="game" saveId="old-save" restoreWarning="core_changed" />);
  expect(screen.getByRole("button", { name: "开始游戏" })).toHaveAccessibleDescription("核心版本已变化，确认后可尝试加载");
  fireEvent.click(screen.getByRole("button", { name: "开始游戏" }));
  expect(screen.getByRole("alertdialog")).toHaveTextContent("原存档会保留");
  expect(calls.create).not.toHaveBeenCalled();
  expect(fullscreen).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "取消" }));
  expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "开始游戏" })).toBeEnabled();
  expect(calls.create).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "开始游戏" }));
  fireEvent.click(screen.getByRole("button", { name: "尝试加载" }));
  await waitFor(() => expect(calls.create).toHaveBeenCalledOnce());
  expect(fullscreen).toHaveBeenCalledOnce();
});

it("confirms a new server warning even if the list was fetched before the core update", async () => {
  calls.create.mockResolvedValue({ data: { id: "new-run", save: { restoreWarning: "core_changed" } }, response: new Response() });
  window.history.replaceState(null, "", "/games/game");
  render(<LaunchButton gameId="game" saveId="old-save" />);
  fireEvent.click(screen.getByRole("button", { name: "开始游戏" }));
  expect(await screen.findByRole("alertdialog")).toHaveTextContent("尝试加载旧存档");
  expect(window.location.pathname).toBe("/games/game");
  fireEvent.click(screen.getByRole("button", { name: "尝试加载" }));
  await waitFor(() => expect(window.location.pathname).toBe("/play/new-run"));
});

it("cancels a late server warning, releases the unused Run and restores launch focus", async () => {
  calls.create.mockResolvedValue({ data: { id: "new-run", save: { restoreWarning: "core_changed" } }, response: new Response() });
  window.history.replaceState(null, "", "/saves?gameId=game");
  render(<LaunchButton gameId="game" saveId="old-save" />);
  fireEvent.click(screen.getByRole("button", { name: "开始游戏" }));
  await screen.findByRole("alertdialog");
  fireEvent.click(screen.getByRole("button", { name: "取消" }));
  await waitFor(() => expect(calls.remove).toHaveBeenCalledExactlyOnceWith("/api/v1/runs/{runId}", { params: { path: { runId: "new-run" } } }));
  expect(window.location.pathname + window.location.search).toBe("/saves?gameId=game");
  await waitFor(() => expect(screen.getByRole("button", { name: "开始游戏" })).toHaveFocus());
});
