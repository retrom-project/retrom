import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AppShell } from "./app-shell";

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
      user: { userId: "user-1", username: "test", displayName: "Test", role: "ADMIN" }
    },
    logout: vi.fn()
  })
}));

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
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
});
