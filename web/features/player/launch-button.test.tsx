import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LaunchButton } from "./launch-button";

vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: vi.fn() }) }));

describe("LaunchButton thread capability guard", () => {
  afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

  it("does not send an impossible threaded launch", async () => {
    const user = userEvent.setup();
    const fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    vi.stubGlobal("isSecureContext", false);
    vi.stubGlobal("crossOriginIsolated", false);
    vi.stubGlobal("SharedArrayBuffer", undefined);
    render(<LaunchButton gameId="game-1" coreId="ppsspp" requiresThreads />);
    await user.click(screen.getByRole("button", { name: "开始游戏" }));

    expect(fetchMock).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent("远程明文 HTTP 无法提供 SharedArrayBuffer");
  });
});
