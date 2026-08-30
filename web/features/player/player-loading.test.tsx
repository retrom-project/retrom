import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { PlayerLoading } from "./player-loading";

afterEach(cleanup);

describe("PlayerLoading", () => {
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
