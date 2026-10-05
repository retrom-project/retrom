import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { PlayerLoading } from "./player-loading";

vi.mock("@/features/auth/auth-provider", () => ({useAuth: () => ({context: {user: {userId: "test"}}})}));

afterEach(cleanup);

describe("PlayerLoading", () => {
  it("offers a config retry without claiming that credentials or dependencies expired", () => {
    const retry = vi.fn();
    render(<PlayerLoading onRetry={retry} message="PLAYER_LAUNCH_SERVICE_UNAVAILABLE" progress={null} returnIntent={{kind: "LIBRARY", href: "/library"}} state="error" />);
    expect(screen.getByText("服务器暂时无法提供启动配置，请稍后重试。")).toBeVisible();
    fireEvent.click(screen.getByRole("button", {name: "重试启动"})); expect(retry).toHaveBeenCalledOnce();
    expect(screen.queryByText(/LAUNCH_CONFIG_503|凭据可能已过期/)).toBeNull();
  });
  it("requires a new launch after credential expiry without offering a doomed config retry", () => {
    render(<PlayerLoading onRetry={vi.fn()} message="PLAYER_LAUNCH_SESSION_EXPIRED" progress={null} returnIntent={{kind: "LIBRARY", href: "/library"}} state="error" />);
    expect(screen.getByText("本次启动凭据已失效，请返回游戏页面重新启动。")).toBeVisible();
    expect(screen.queryByRole("button", {name: "重试启动"})).toBeNull();
    expect(screen.getByRole("link", {name: "返回游戏库"})).toHaveAttribute("href", "/library");
  });
  it("stops pending task spinners after a terminal startup failure", () => {
    const task = {id: "core", kind: "GAME_START" as const, state: "RUNNING" as const, progress: null, summary: true};
    const {container} = render(<PlayerLoading onRetry={() => undefined} message="PLAYER_RESOURCE_IDLE_TIMEOUT" progress={null} returnIntent={{kind: "LIBRARY", href: "/library"}} state="error" tasks={[task]} />);
    expect(screen.getByText("游戏启动失败")).toBeVisible();
    expect(container.querySelector('[data-task-state="RUNNING"]')).toBeNull();
    expect(container.querySelector(".player-startup-spinner")).toBeNull();
    expect(task.state).toBe("RUNNING");
  });
  it.each([
    ["PLAYER_RESOURCE_IDLE_TIMEOUT", "资源下载已停止推进，请检查网络后重试。", true],
    ["PLAYER_CORE_INITIALIZATION_TIMEOUT", "资源已就绪，但核心初始化超时。请重试启动。", true],
    ["PLAYER_RUNTIME_CSP_BLOCKED", "运行资源被浏览器安全策略阻止，请联系管理员更新运行依赖。", false],
  ])("explains the public startup failure %s", (message, title, retryable) => {
    render(<PlayerLoading onRetry={() => undefined} message={message} progress={null} returnIntent={{kind: "LIBRARY", href: "/library"}} state="error" />);
    expect(screen.getByText(title)).toBeVisible();
    expect(Boolean(screen.queryByRole("button", {name: "重试启动"}))).toBe(retryable);
    expect(screen.queryByText("凭据可能已过期或依赖不兼容。")).toBeNull();
    expect(screen.getByRole("link", {name: "返回游戏库"})).toBeVisible();
  });
  it.each(["CACHE_UNAVAILABLE", "WORKSPACE_UNAVAILABLE", "NETWORK_FAILED", "TIMEOUT"])("offers explicit retry and streaming fallback after %s", code => {
    render(<PlayerLoading onRetry={() => undefined} canLoadOnDemand message={`CONTENT_IO_${code}`} progress={null} returnIntent={{kind: "LIBRARY", href: "/library"}} state="error" />);
    expect(screen.getByRole("button", {name: "重试下载"})).toBeVisible();
    expect(screen.getByRole("button", {name: "改为按需加载"})).toBeVisible();
    expect(screen.getByText("已下载的有效内容会在重试时复用。")).toBeVisible();
    expect(screen.queryByRole("progressbar")).toBeNull();
  });
  it("shows aggregate byte progress while uncached runtime content is loading", () => {
    render(<PlayerLoading onRetry={() => undefined}
      message="正在启动 ONScripter 运行时…"
      progress={{ loadedBytes: 384 * 1024 * 1024, totalBytes: 768 * 1024 * 1024 }}
      returnIntent={{kind: "LIBRARY", href: "/library"}}
      state="loading"
    />);

    const progress = screen.getByRole("progressbar", { name: "游戏内容进度" });
    expect(progress).toHaveAttribute("aria-valuenow", "50");
    expect(progress.querySelectorAll(".is-filled")).toHaveLength(5);
    expect(screen.getByText("游戏启动中", {selector: "strong"})).toBeVisible();
    expect(screen.getByText(/首次加载会写入本地缓存/u)).toBeInTheDocument();
  });

  it("does not expose a misleading progress bar before a byte total is known", () => {
    render(<PlayerLoading onRetry={() => undefined}
      message="正在验证运行快照…"
      progress={null}
      returnIntent={{kind: "IMMERSIVE_HOME", href: "/immersive"}}
      state="loading"
    />);

    expect(screen.queryByRole("progressbar")).toBeNull();
  });
});

it("does not offer a demand fallback for full-download-only or unmanaged targets", () => {
  render(<PlayerLoading onRetry={() => undefined} canLoadOnDemand={false} message="CONTENT_IO_CACHE_UNAVAILABLE" progress={null} returnIntent={{kind: "LIBRARY", href: "/library"}} state="error" />);
  expect(screen.getByRole("button", {name: "重试下载"})).toBeVisible();
  expect(screen.queryByRole("button", {name: "改为按需加载"})).toBeNull();
});

it.each(["OPENBOR_CORE_EXITED", "RUNTIME_SESSION_UNAVAILABLE"])("keeps the return intent on %s", message => {
  render(<PlayerLoading onRetry={vi.fn()} message={message} progress={null} state="error" returnIntent={{kind:"GAME",href:"/games/game-1"}} />);
  expect(screen.getByRole("link", {name:"返回游戏详情"})).toHaveAttribute("href", "/games/game-1");
  expect(screen.queryByRole("link", {name:"返回游戏库"})).not.toBeInTheDocument();
});
