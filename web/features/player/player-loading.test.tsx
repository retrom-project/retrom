import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { PlayerLoading } from "./player-loading";

vi.mock("@/features/auth/auth-provider", () => ({useAuth: () => ({context: {user: {userId: "test"}}})}));

afterEach(cleanup);

describe("PlayerLoading", () => {
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

    const progress = screen.getByRole("progressbar", { name: "游戏内容加载进度" });
    expect(progress).toHaveAttribute("aria-valuenow", "50");
    expect(screen.getByText("384.0 MiB / 768.0 MiB · 50%")).toBeInTheDocument();
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
