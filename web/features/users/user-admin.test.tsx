import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { UserAdmin, type AdminUser, type LinkPage, type UserPage } from "./user-admin";

const auth = vi.hoisted(() => ({ fetch: vi.fn() }));
const router = vi.hoisted(() => ({ push: vi.fn(), refresh: vi.fn() }));
vi.mock("@/features/auth/auth-provider", () => ({ useAuth: () => ({ context: { user: { userId: "admin-1", username: "admin", role: "ADMIN" } }, authenticatedFetch: auth.fetch }) }));
vi.mock("next/navigation", () => ({ useRouter: () => router }));

const admin: AdminUser = { userId: "admin-1", username: "admin", displayName: "Server Admin", role: "ADMIN", status: "ENABLED", version: 1, createdAtMs: 1_780_000_000_000, lastLoginAtMs: 1_780_000_100_000, activeSessionCount: 1 };
const alice: AdminUser = { userId: "user-2", username: "alice", displayName: "Alice", role: "USER", status: "ENABLED", version: 2, createdAtMs: 1_780_000_200_000, lastLoginAtMs: null, activeSessionCount: 0 };
const initialUsers: UserPage = { generatedAtMs: 1_780_000_300_000, items: [admin, alice], nextCursor: null };
const initialInvitations: LinkPage = { generatedAtMs: 1_780_000_300_000, items: [], nextCursor: null };

function json(value: unknown, status = 200, headers: Record<string, string> = {}) {
  return new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json", ...headers } });
}

beforeEach(() => {
  auth.fetch.mockReset(); router.push.mockReset(); router.refresh.mockReset();
  Object.defineProperty(window.navigator, "onLine", { configurable: true, value: true });
});
afterEach(() => { cleanup(); });

describe("UserAdmin", () => {
  it("shows only account security fields and never private game metrics", () => {
    render(<UserAdmin initialUsers={initialUsers} initialInvitations={initialInvitations} filterValues={{}} />);
    expect(screen.getByRole("heading", { name: "用户管理" })).toBeInTheDocument();
    expect(screen.getByText("活跃会话")).toBeInTheDocument();
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.queryByText(/游戏数|游玩时长|存档数|Profile ID|IP/)).not.toBeInTheDocument();
  });

  it("disables self-management and explains the last-admin fence", async () => {
    auth.fetch.mockImplementation(async (input: RequestInfo | URL) => String(input).includes("role=ADMIN")
      ? json({ ...initialUsers, items: [admin] })
      : json(admin, 200, { ETag: '"v1"' }));
    const user = userEvent.setup();
    render(<UserAdmin initialUsers={initialUsers} initialInvitations={initialInvitations} filterValues={{}} />);
    const adminRow = screen.getByText("Server Admin").closest("tr")!;
    await user.click(within(adminRow).getByRole("button", { name: "管理" }));
    const drawer = await screen.findByRole("dialog", { name: "管理用户" });
    expect(await screen.findByText("不能修改当前登录账号")).toBeInTheDocument();
    expect(screen.getByText("服务器必须保留至少一名启用管理员")).toBeInTheDocument();
    expect(within(drawer).getByLabelText("角色")).toBeDisabled();
    expect(within(drawer).getByRole("button", { name: "删除账号" })).toBeDisabled();
  });

  it("uses ETag and explicit deactivation confirmation when changing another user", async () => {
    const disabled = { ...alice, status: "DISABLED" as const, version: 3 };
    auth.fetch.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("role=ADMIN")) {return json({ ...initialUsers, items: [admin] });}
      if (url.endsWith("/user-2") && !init?.method) {return json(alice, 200, { ETag: '"v2"' });}
      if (url.endsWith("/user-2") && init?.method === "PATCH") {return json(disabled, 200, { ETag: '"v3"' });}
      throw new Error(`unexpected ${url}`);
    });
    const user = userEvent.setup();
    render(<UserAdmin initialUsers={initialUsers} initialInvitations={initialInvitations} filterValues={{}} />);
    await user.click(within(screen.getByText("Alice").closest("tr")!).getByRole("button", { name: "管理" }));
    const drawer = await screen.findByRole("dialog", { name: "管理用户" });
    await user.selectOptions(within(drawer).getByLabelText("状态"), "DISABLED");
    await user.click(within(drawer).getByRole("button", { name: "保存更改" }));
    const dialog = screen.getByRole("alertdialog", { name: "确认停用账号" });
    expect(dialog).toHaveTextContent("立即退出所有设备");
    await user.click(within(dialog).getByRole("button", { name: "停用账号" }));
    await waitFor(() => expect(auth.fetch).toHaveBeenCalledWith("/api/v1/admin/users/user-2", expect.objectContaining({ method: "PATCH", headers: expect.objectContaining({ "If-Match": '"v2"' }) })));
    expect(screen.getByText("账号安全状态已更新")).toBeInTheDocument();
  });

  it("destroys a one-time invitation URL after the result dialog closes", async () => {
    const secretURL = "http://retrom.local/register#invite=one-time-secret";
    auth.fetch.mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input) === "/api/v1/admin/invitations" && init?.method === "POST") {return json({ url: secretURL, role: "USER", expiresAtMs: 1_900_000_000_000 }, 201);}
      if (String(input).startsWith("/api/v1/admin/invitations?")) {return json(initialInvitations);}
      throw new Error(`unexpected ${String(input)}`);
    });
    const user = userEvent.setup();
    render(<UserAdmin initialUsers={initialUsers} initialInvitations={initialInvitations} filterValues={{}} />);
    await user.click(screen.getByRole("button", { name: "创建邀请" }));
    const drawer = screen.getByRole("dialog", { name: "创建邀请" });
    await user.click(within(drawer).getByRole("button", { name: "创建邀请" }));
    const result = await screen.findByRole("alertdialog", { name: "邀请已创建" });
    expect(within(result).getByDisplayValue(secretURL)).toBeInTheDocument();
    await user.click(within(result).getByRole("button", { name: "完成" }));
    await waitFor(() => expect(screen.queryByDisplayValue(secretURL)).not.toBeInTheDocument());
    expect(auth.fetch).toHaveBeenCalledWith("/api/v1/admin/invitations?state=ACTIVE&limit=50", expect.objectContaining({ cache: "no-store" }));
  });
});
