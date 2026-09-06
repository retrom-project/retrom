import { act, cleanup, render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AppShell } from "@/components/app-shell";
import { MobileProfile } from "./mobile-profile";

const state = vi.hoisted(() => ({ pathname: "/", role: "ADMIN", netplayEnabled: false }));
vi.mock("next/link", () => ({
  default: ({ children, href, ...props }: { children: ReactNode; href: string }) => <a href={href} {...props}>{children}</a>,
  useLinkStatus: () => ({ pending: false }),
}));
vi.mock("next/navigation", () => ({ usePathname: () => state.pathname }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({
  context: { instanceState: "READY", authenticationState: "AUTHENTICATED", netplayEnabled: state.netplayEnabled,
    user: { userId: "one", username: "one", displayName: "玩家", role: state.role } },
  logout: vi.fn(),
}) }));

let phone = true;
const listeners = new Set<() => void>();
beforeEach(() => {
  phone = true;
  state.pathname = "/";
  state.role = "ADMIN";
  state.netplayEnabled = false;
  vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 200 })));
  vi.stubGlobal("matchMedia", vi.fn(() => ({
    matches: phone,
    addEventListener: (_type: string, listener: () => void) => listeners.add(listener),
    removeEventListener: (_type: string, listener: () => void) => listeners.delete(listener),
  })));
});
afterEach(() => { cleanup(); listeners.clear(); vi.unstubAllGlobals(); });

describe("phone application boundary", () => {
  it("uses three destinations and does not expose administration or service diagnostics", () => {
    render(<AppShell><h1>游戏</h1></AppShell>);
    expect(within(screen.getByRole("navigation", { name: "手机主导航" })).getAllByRole("link").map((link) => link.textContent)).toEqual(["首页", "游戏库", "我的"]);
    expect(screen.queryByRole("link", { name: "管理后台" })).not.toBeInTheDocument();
    expect(screen.queryByLabelText("打开主要导航")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("服务正常")).not.toBeInTheDocument();
  });

  it("keeps game details in the library and personal destinations in My", () => {
    state.pathname = "/games/one";
    const view = render(<AppShell><h1>游戏详情</h1></AppShell>);
    const nav = screen.getByRole("navigation", { name: "手机主导航" });
    expect(within(nav).getByRole("link", { name: "游戏库" })).toHaveAttribute("aria-current", "page");
    state.pathname = "/saves";
    view.rerender(<AppShell><h1>我的存档</h1></AppShell>);
    expect(within(nav).getByRole("link", { name: "我的" })).toHaveAttribute("aria-current", "page");
  });

  it("does not mount admin controls on a phone and restores the same page on desktop", () => {
    state.pathname = "/admin/imports";
    render(<AppShell><button type="button">导入测试文件</button></AppShell>);
    expect(screen.getByRole("heading", { name: "请在电脑上管理游戏库" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "导入测试文件" })).not.toBeInTheDocument();
    act(() => { phone = false; for (const listener of listeners) {listener();} });
    expect(screen.getByRole("button", { name: "导入测试文件" })).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "主要导航" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "请在电脑上管理游戏库" })).not.toBeInTheDocument();
  });

  it("retains the authorization boundary for ordinary users", () => {
    state.pathname = "/admin/users";
    state.role = "USER";
    render(<AppShell><button type="button">私有管理操作</button></AppShell>);
    expect(screen.getByRole("heading", { name: "没有管理权限" })).toBeInTheDocument();
    expect(screen.queryByText("私有管理操作")).not.toBeInTheDocument();
    expect(screen.queryByText("请在电脑上管理游戏库")).not.toBeInTheDocument();
  });

  it.each(["/immersive", "/immersive/library/all", "/play/one"])("leaves the standalone experience untouched at %s", (pathname) => {
    state.pathname = pathname;
    const { container } = render(<AppShell><main>独立界面</main></AppShell>);
    expect(container.querySelector(".phone-app-frame")).toBeNull();
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
  });

  it("groups personal game tools and honors the netplay feature flag", () => {
    const view = render(<MobileProfile />);
    for (const label of ["我的收藏", "我的存档", "最近游玩", "账户设置"]) {
      expect(screen.getByRole("link", { name: label })).toBeInTheDocument();
    }
    expect(screen.queryByRole("link", { name: "管理后台" })).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "联机游玩" })).not.toBeInTheDocument();
    state.netplayEnabled = true;
    view.rerender(<MobileProfile />);
    expect(screen.getByRole("link", { name: "联机游玩" })).toBeInTheDocument();
  });
});
