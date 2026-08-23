import { act, cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ControllerSnapshot, ControllerSource } from "./types";
import { GamepadProvider } from "./provider";

const navigation = vi.hoisted(() => ({ pathname: "/", push: vi.fn() }));

vi.mock("next/link", () => ({
  default: ({ children, href, ...props }: { children: ReactNode; href: string }) =>
    <a href={href} {...props}>{children}</a>,
}));
vi.mock("next/navigation", () => ({
  usePathname: () => navigation.pathname,
  useRouter: () => ({ push: navigation.push }),
}));
vi.mock("@/features/auth/auth-provider", () => ({
  useAuth: () => ({
    context: {
      authenticationState: "AUTHENTICATED",
      netplayEnabled: true,
    },
  }),
}));

function snapshot(pressed: number[] = []): ControllerSnapshot {
  const active = new Set(pressed);
  return {
    index: 0,
    connected: true,
    mapping: "standard",
    timestamp: 1,
    buttons: Array.from({ length: 17 }, (_, index) => ({
      pressed: active.has(index),
      touched: active.has(index),
      value: active.has(index) ? 1 : 0,
    })),
    axes: [0, 0, 0, 0],
  };
}

function source(initial: ControllerSnapshot | null) {
  let current = initial;
  return {
    value: { read: () => [current] } satisfies ControllerSource,
    set(next: ControllerSnapshot | null) {current = next;},
  };
}

beforeEach(() => {
  vi.useFakeTimers();
  navigation.pathname = "/";
  navigation.push.mockReset();
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({
    x: 0, y: 0, width: 180, height: 48, top: 0, right: 180, bottom: 48, left: 0,
    toJSON: () => ({}),
  });
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) =>
    window.setTimeout(() => callback(performance.now()), 16));
  vi.stubGlobal("cancelAnimationFrame", (id: number) => window.clearTimeout(id));
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("GamepadProvider", () => {
  it("claims without activating, waits for neutral and then drives real page focus", async () => {
    const pad = source(snapshot([0]));
    const activate = vi.fn();
    render(<GamepadProvider source={pad.value}><main><button type="button" onClick={activate}>继续游戏</button></main></GamepadProvider>);

    await act(() => vi.advanceTimersByTime(20));
    expect(screen.getByText("手柄已连接")).toBeVisible();
    expect(activate).not.toHaveBeenCalled();

    pad.set(snapshot());
    await act(() => vi.advanceTimersByTime(160));
    expect(screen.getByRole("button", { name: "继续游戏" })).toHaveFocus();

    pad.set(snapshot([0]));
    await act(() => vi.advanceTimersByTime(20));
    expect(activate).toHaveBeenCalledOnce();
  });

  it("opens user navigation with Start and never exposes an administrator destination", async () => {
    const pad = source(snapshot([9]));
    render(<GamepadProvider source={pad.value}><main><button type="button">浏览游戏库</button></main></GamepadProvider>);
    await act(() => vi.advanceTimersByTime(20));
    pad.set(snapshot());
    await act(() => vi.advanceTimersByTime(160));
    pad.set(snapshot([9]));
    await act(() => vi.advanceTimersByTime(20));

    expect(screen.getByRole("dialog", { name: "用户导航" })).toBeVisible();
    expect(screen.queryByRole("link", { name: "管理后台" })).not.toBeInTheDocument();
  });

  it("does not poll or react on administrator routes", async () => {
    navigation.pathname = "/admin/games";
    const pad = source(snapshot([0]));
    const activate = vi.fn();
    render(<GamepadProvider source={pad.value}><main><button type="button" onClick={activate}>管理</button></main></GamepadProvider>);
    await act(() => vi.advanceTimersByTime(200));
    expect(activate).not.toHaveBeenCalled();
    expect(screen.queryByText("手柄已连接")).not.toBeInTheDocument();
  });
});
