import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { PlayerLoading } from "./player-loading";

vi.mock("@/features/auth/auth-provider", () => ({useAuth: () => ({context: {user: {userId: "test"}}})}));

afterEach(cleanup);

describe("PlayerLoading", () => {
  it("stops pending task spinners after a terminal startup failure", () => {
    const task = {id: "core", kind: "GAME_START" as const, state: "RUNNING" as const, progress: null, summary: true};
    const {container} = render(<PlayerLoading immersive={false} message="PLAYER_RESOURCE_IDLE_TIMEOUT" progress={null} returnTo="/library" state="error" tasks={[task]} />);
    expect(screen.getByText("游戏启动失败")).toBeVisible();
    expect(container.querySelector('[data-task-state="RUNNING"]')).toBeNull();
    expect(container.querySelector(".player-startup-spinner")).toBeNull();
    expect(task.state).toBe("RUNNING");
  });
  it.each([
    ["PLAYER_RESOURCE_IDLE_TIMEOUT", "资源下载已停止推进，请检查网络后重试。"],
    ["PLAYER_CORE_INITIALIZATION_TIMEOUT", "资源已就绪，但核心初始化超时。请重试启动。"],
    ["PLAYER_RUNTIME_CSP_BLOCKED", "运行资源被浏览器安全策略阻止，请联系管理员更新运行依赖。"],
  ])("explains the public startup failure %s", (message, title) => {
    render(<PlayerLoading immersive={false} message={message} progress={null} returnTo="/library" state="error" />);
    expect(screen.getByText(title)).toBeVisible();
    expect(screen.getByRole("button", {name: "重试启动"})).toBeVisible();
    expect(screen.queryByText("凭据可能已过期或依赖不兼容。")).toBeNull();
    expect(screen.getByRole("link", {name: "返回游戏库"})).toBeVisible();
  });
  it.each(["CACHE_UNAVAILABLE", "WORKSPACE_UNAVAILABLE", "NETWORK_FAILED", "TIMEOUT"])("offers explicit retry and streaming fallback after %s", code => {
    render(<PlayerLoading canLoadOnDemand immersive={false} message={`CONTENT_IO_${code}`} progress={null} returnTo="/library" state="error" />);
    expect(screen.getByRole("button", {name: "重试下载"})).toBeVisible();
    expect(screen.getByRole("button", {name: "改为按需加载"})).toBeVisible();
    expect(screen.getByText("已下载的有效内容会在重试时复用。")).toBeVisible();
    expect(screen.queryByRole("progressbar")).toBeNull();
  });
  it("shows aggregate byte progress while uncached runtime content is loading", () => {
    render(<PlayerLoading
      immersive={false}
      message="正在启动 ONScripter 运行时…"
      progress={{ loadedBytes: 384 * 1024 * 1024, totalBytes: 768 * 1024 * 1024 }}
      returnTo="/library"
      state="loading"
    />);

    const progress = screen.getByRole("progressbar", { name: "游戏内容进度" });
    expect(progress).toHaveAttribute("aria-valuenow", "50");
    expect(progress.querySelectorAll(".is-filled")).toHaveLength(5);
    expect(screen.getByText("游戏启动中", {selector: "strong"})).toBeVisible();
    expect(screen.getByText(/首次加载会写入本地缓存/u)).toBeInTheDocument();
  });

  it("does not expose a misleading progress bar before a byte total is known", () => {
    render(<PlayerLoading
      immersive
      message="正在验证运行快照…"
      progress={null}
      returnTo="/immersive"
      state="loading"
    />);

    expect(screen.queryByRole("progressbar")).toBeNull();
  });
});

it("does not offer a demand fallback for full-download-only or unmanaged targets", () => {
  render(<PlayerLoading canLoadOnDemand={false} immersive={false} message="CONTENT_IO_CACHE_UNAVAILABLE" progress={null} returnTo="/library" state="error" />);
  expect(screen.getByRole("button", {name: "重试下载"})).toBeVisible();
  expect(screen.queryByRole("button", {name: "改为按需加载"})).toBeNull();
});
