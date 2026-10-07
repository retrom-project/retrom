import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import type * as ApiClient from "@/lib/api/client";
import { LaunchButton } from "./launch-button";

const calls = vi.hoisted(() => ({ create: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({
  push: (path: string) => window.history.pushState(null, "", path),
  replace: (path: string) => window.history.replaceState(null, "", path),
}) }));
vi.mock("@/lib/api/client", async (importOriginal) => {
  const actual = await importOriginal<typeof ApiClient>();
  return { ...actual, api: { POST: calls.create } };
});
beforeEach(() => { calls.create.mockReset(); calls.create.mockResolvedValue({ data: { id: "new-run" }, response: new Response() }); });
afterEach(cleanup);

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
