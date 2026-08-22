import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AppShell } from "./app-shell";

const shellState = vi.hoisted(() => ({ pathname: "/", role: "ADMIN" as "ADMIN" | "USER", netplayEnabled: false }));

vi.mock("next/link", () => ({
  default: ({ children, href, ...props }: { children: ReactNode; href: string }) => <a href={href} {...props}>{children}</a>,
  useLinkStatus: () => ({ pending: false })
}));

vi.mock("next/navigation", () => ({ usePathname: () => shellState.pathname }));

vi.mock("@/features/auth/auth-provider", () => ({
  useAuth: () => ({
    context: {
      instanceState: "READY",
      authenticationState: "AUTHENTICATED",
      netplayEnabled: shellState.netplayEnabled,
      user: { userId: "user-1", username: "test", displayName: "Test", role: shellState.role }
    },
    logout: vi.fn()
  })
}));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  shellState.pathname = "/";
  shellState.role = "ADMIN";
  shellState.netplayEnabled = false;
});

describe("AppShell", () => {
  it("closes the account menu when the user clicks outside it", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 200 })));
    const user = userEvent.setup();
    render(<AppShell><button type="button">页面内容</button></AppShell>);

    const summary = screen.getByText("Test").closest("summary");
    const menu = summary?.closest("details");
    expect(summary).not.toBeNull();
    expect(menu).not.toBeNull();

    await user.click(summary!);
    expect(menu).toHaveAttribute("open");

    await user.click(screen.getByRole("button", { name: "页面内容" }));
    expect(menu).not.toHaveAttribute("open");
  });

  it("keeps account health together and places the administrator switch at the bottom", () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 200 })));
    const { container } = render(<AppShell><div>页面内容</div></AppShell>);

    const foot = container.querySelector(".sidebar-foot");
    const accountRow = container.querySelector(".sidebar-account-row");
    const switchLink = screen.getByRole("link", { name: "管理后台" });
    expect(accountRow?.querySelector(".account-menu")).not.toBeNull();
    expect(accountRow?.querySelector(".connection")).not.toBeNull();
    expect(Array.from(foot?.children ?? [])).toEqual([accountRow, switchLink]);
  });

  it("shows service health beside personal information for a regular account", () => {
    shellState.role = "USER";
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 200 })));
    const { container } = render(<AppShell><div>页面内容</div></AppShell>);

    const accountRow = container.querySelector(".sidebar-account-row");
    expect(accountRow?.querySelector(".account-menu")).not.toBeNull();
    expect(accountRow?.querySelector(".connection")).not.toBeNull();
    expect(screen.queryByRole("link", { name: "管理后台" })).not.toBeInTheDocument();
    expect(container.querySelector(".sidebar-foot")?.children).toHaveLength(1);
  });

  it("places local scanning between game import and task progress", () => {
    shellState.pathname = "/admin/imports";
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 200 })));
    render(<AppShell><div>页面内容</div></AppShell>);

    const links = screen.getByRole("navigation", { name: "主要导航" }).querySelectorAll("a");
    expect(Array.from(links, (link) => link.textContent).slice(0, 4))
      .toEqual(["游戏入库", "导入游戏", "本地扫描", "任务进度"]);
  });

  it("places storage analysis directly after runtime dependencies and marks it active", () => {
    shellState.pathname = "/admin/storage";
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 200 })));
    render(<AppShell><div>页面内容</div></AppShell>);

    const navigation = screen.getByRole("navigation", { name: "主要导航" });
    const links = Array.from(navigation.querySelectorAll("a"));
    const labels = links.map((link) => link.textContent);
    expect(labels.slice(-2)).toEqual(["运行依赖", "容量分析"]);
    expect(within(navigation).getByRole("link", { name: "容量分析" })).toHaveAttribute("aria-current", "page");
  });

  it("shows netplay immediately after recent games only when enabled", () => {
    shellState.netplayEnabled = true;
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 200 })));
    const { rerender } = render(<AppShell><div>页面内容</div></AppShell>);
    const labels = Array.from(screen.getByRole("navigation", { name: "主要导航" }).querySelectorAll("a"), (link) => link.textContent);
    expect(labels.slice(-2)).toEqual(["最近游玩", "联机游玩"]);
    shellState.netplayEnabled = false;
    rerender(<AppShell><div>页面内容</div></AppShell>);
    expect(screen.queryByRole("link", { name: "联机游玩" })).not.toBeInTheDocument();
  });

  it("keeps detail routes in the library tab and restores focus after More closes", async () => {
    shellState.pathname = "/games/game-1";
    shellState.netplayEnabled = true;
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 200 })));
    const user = userEvent.setup();
    render(<AppShell><div>页面内容</div></AppShell>);

    const bottom = screen.getByRole("navigation", { name: "手机主导航" });
    expect(within(bottom).getByRole("link", { name: "游戏库" })).toHaveAttribute("aria-current", "page");
    const more = within(bottom).getByRole("button", { name: "更多导航" });
    await user.click(more);
    const sheet = screen.getByRole("dialog", { name: "更多" });
    expect(within(sheet).getByRole("link", { name: /最近游玩/ })).toHaveAttribute("href", "/recent");
    expect(within(sheet).getByRole("link", { name: /联机游玩/ })).toHaveAttribute("href", "/netplay");
    await user.click(within(sheet).getByRole("button", { name: "关闭更多" }));
    expect(more).toHaveFocus();
  });

  it("opens the compact administrator navigation with active and context semantics", async () => {
    shellState.pathname = "/admin/reviews";
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 200 })));
    const user = userEvent.setup();
    render(<AppShell><div>页面内容</div></AppShell>);

    const trigger = screen.getByRole("button", { name: "打开主要导航" });
    await user.click(trigger);
    const sheet = screen.getByRole("dialog", { name: "管理后台" });
    expect(within(sheet).getByRole("link", { name: "游戏入库" })).toHaveClass("is-context");
    expect(within(sheet).getByRole("link", { name: "待审核" })).toHaveAttribute("aria-current", "page");
    await user.keyboard("{Escape}");
    expect(trigger).toHaveFocus();
  });
});
