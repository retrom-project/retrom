import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AppShell } from "./app-shell";

const authState = vi.hoisted(() => ({ role: "ADMIN" as "ADMIN" | "USER" }));

vi.mock("next/link", () => ({
  default: ({ children, href, ...props }: { children: ReactNode; href: string }) => <a href={href} {...props}>{children}</a>,
  useLinkStatus: () => ({ pending: false })
}));

vi.mock("next/navigation", () => ({ usePathname: () => "/" }));

vi.mock("@/features/auth/auth-provider", () => ({
  useAuth: () => ({
    context: {
      instanceState: "READY",
      authenticationState: "AUTHENTICATED",
      user: { userId: "user-1", username: "test", displayName: "Test", role: authState.role }
    },
    logout: vi.fn()
  })
}));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  authState.role = "ADMIN";
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
    authState.role = "USER";
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 200 })));
    const { container } = render(<AppShell><div>页面内容</div></AppShell>);

    const accountRow = container.querySelector(".sidebar-account-row");
    expect(accountRow?.querySelector(".account-menu")).not.toBeNull();
    expect(accountRow?.querySelector(".connection")).not.toBeNull();
    expect(screen.queryByRole("link", { name: "管理后台" })).not.toBeInTheDocument();
    expect(container.querySelector(".sidebar-foot")?.children).toHaveLength(1);
  });
});
