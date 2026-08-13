import { cleanup, render, screen } from "@testing-library/react";
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
});
