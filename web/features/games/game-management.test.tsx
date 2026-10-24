import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import type * as ApiClient from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { GameManagement } from "./game-management";

const calls = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn() }) }));
vi.mock("@/lib/api/client", async (original) => ({
  ...await original<typeof ApiClient>(), api: { GET: calls.get, POST: calls.post },
}));
afterEach(() => { cleanup(); vi.clearAllMocks(); });

const detail = { game: { id: "game", version: 4 } } as Schema<"GameDetail">;
const running = {
  id: "replacement", gameId: "game", status: "running", phase: "preparing",
  canCancel: true, result: null, error: null, createdAtMs: 1, updatedAtMs: 1, expiresAtMs: 100000,
};

it("does not announce completion or reload the game when replacement is only accepted", async () => {
  const onChange = vi.fn();
  calls.get.mockResolvedValue({ response: new Response(null, { status: 404 }), error: { code: "NOT_FOUND", message: "Unavailable" } });
  calls.post.mockResolvedValue({ response: new Response(null, { status: 202 }), data: running });
  render(<GameManagement detail={detail} onChange={onChange} />);
  await waitFor(() => expect(screen.getByRole("button", { name: "替换游戏内容" })).toBeEnabled());
  fireEvent.click(screen.getByRole("button", { name: "替换游戏内容" }));
  fireEvent.change(screen.getByRole("textbox", { name: "服务器内容路径" }), { target: { value: "/games/new.zip" } });
  fireEvent.click(screen.getByRole("button", { name: "确认" }));
  await waitFor(() => expect(calls.post).toHaveBeenCalled());
  await waitFor(() => expect(screen.getByText("正在准备游戏内容")).toBeVisible());
  expect(onChange).not.toHaveBeenCalled();
  expect(screen.queryByText("游戏内容已替换")).not.toBeInTheDocument();
});
