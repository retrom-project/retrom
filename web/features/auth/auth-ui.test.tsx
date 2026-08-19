import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthProvider } from "./auth-provider";
import { AccountSettings, LoginForm, RegisterForm, SetupForm } from "./auth-ui";
import type { AuthContext } from "./types";

const navigation = vi.hoisted(() => ({ pathname: "/login", replace: vi.fn(), refresh: vi.fn() }));
vi.mock("next/navigation", () => ({ usePathname: () => navigation.pathname, useRouter: () => navigation }));

const anonymous: AuthContext = {
  instanceState: "READY", mode: "test", authenticationState: "UNAUTHENTICATED", user: null, csrfToken: null,
  idleExpiresAtMs: null, absoluteExpiresAtMs: null, testDefaultAccountActive: true, netplayEnabled: false
};
const registered: AuthContext = {
  ...anonymous, authenticationState: "AUTHENTICATED", csrfToken: "csrf", user: { userId: "user-1", username: "alice", displayName: "Alice", role: "USER" }
};

function wrapped(node: React.ReactNode, initialContext = anonymous) {
  return render(<AuthProvider initialContext={initialContext}>{node}</AuthProvider>);
}

beforeEach(() => {
  navigation.replace.mockReset(); navigation.refresh.mockReset(); navigation.pathname = "/login";
  window.history.replaceState(null, "", "/login");
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("authentication UI", () => {
  it("submits every setup credential and enters the authenticated context", async () => {
    navigation.pathname = "/setup";
    window.history.replaceState(null, "", "/setup");
    const fetchMock = vi.fn(async () => new Response(JSON.stringify(registered), { status: 201, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    wrapped(<SetupForm />, { ...anonymous, instanceState: "INITIALIZATION_REQUIRED", mode: "release", authenticationState: "NOT_APPLICABLE" });
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("初始化码", { selector: "input" }), "setup-proof");
    await user.type(screen.getByLabelText("管理员用户名"), "admin");
    await user.type(screen.getByLabelText("显示名称"), "Server Admin");
    const password = "A1!x2z";
    const passwordInput = screen.getByLabelText("密码", { selector: "input" });
    const confirmationInput = screen.getByLabelText("确认密码", { selector: "input" });
    expect(passwordInput).toHaveAttribute("minlength", "6");
    expect(confirmationInput).toHaveAttribute("minlength", "6");
    expect(screen.getByText("至少 6 个字符，可以使用空格；不要求特定字符组合。")).toBeInTheDocument();
    await user.type(passwordInput, password);
    await user.type(confirmationInput, password);
    await user.click(screen.getByRole("button", { name: "创建管理员并进入 Retrom" }));
    await waitFor(() => expect(navigation.replace).toHaveBeenCalledWith("/"));
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/auth/initialize", expect.objectContaining({
      method: "POST", body: expect.stringContaining('"setupCode":"setup-proof"')
    }));
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/auth/initialize", expect.objectContaining({
      method: "POST", body: expect.stringContaining('"password":"A1!x2z"')
    }));
  });

  it("shows the test credential warning and normalizes login failures", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: { code: "AUTHENTICATION_FAILED", message: "internal detail" } }), { status: 401, headers: { "Content-Type": "application/json" } })));
    wrapped(<LoginForm />);
    expect(screen.getByText(/默认管理员为 test \/ test/)).toBeInTheDocument();
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("用户名"), "missing");
    await user.type(screen.getByLabelText("密码"), "wrong");
    await user.click(screen.getByRole("button", { name: "登录" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("用户名或密码不正确");
    expect(screen.getByLabelText("用户名")).toHaveValue("missing");
    expect(screen.getByLabelText("密码")).toHaveValue("");
  });

  it("removes invitation capability from the URL before inspection and never stores it", async () => {
    navigation.pathname = "/register";
    window.history.replaceState(null, "", "/register#invite=secret-capability");
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ kind: "INVITATION", role: "USER", username: null, expiresAtMs: 1_900_000_000_000 }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    wrapped(<RegisterForm />);
    expect(await screen.findByText("普通用户")).toBeInTheDocument();
    expect(window.location.hash).toBe("");
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/auth/account-links/inspect", expect.objectContaining({ body: expect.stringContaining("secret-capability") }));
    expect(JSON.stringify({ ...localStorage })).not.toContain("secret-capability");
    expect(JSON.stringify({ ...sessionStorage })).not.toContain("secret-capability");
  });

  it("accepts an invitation from component memory and rotates into the authenticated context", async () => {
    navigation.pathname = "/register";
    window.history.replaceState(null, "", "/register#invite=secret-capability");
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      void init;
      return String(input).endsWith("/inspect")
        ? new Response(JSON.stringify({ kind: "INVITATION", role: "USER", username: null, expiresAtMs: 1_900_000_000_000 }), { status: 200, headers: { "Content-Type": "application/json" } })
        : new Response(JSON.stringify(registered), { status: 201, headers: { "Content-Type": "application/json" } });
    });
    vi.stubGlobal("fetch", fetchMock);
    wrapped(<RegisterForm />);
    const user = userEvent.setup();
    await screen.findByText("普通用户");
    await user.type(screen.getByLabelText("用户名"), "alice");
    await user.type(screen.getByLabelText("显示名称"), "Alice");
    await user.type(screen.getByLabelText("密码", { selector: "input" }), "a sufficiently long password");
    await user.type(screen.getByLabelText("确认密码", { selector: "input" }), "a sufficiently long password");
    await user.click(screen.getByRole("button", { name: "创建账号并进入 Retrom" }));
    await waitFor(() => expect(navigation.replace).toHaveBeenCalledWith("/"));
    const acceptCall = fetchMock.mock.calls.find(([input]) => String(input).endsWith("/accept"));
    expect(acceptCall?.[1]?.body).toContain("secret-capability");
  });

  it("keeps account identity read-only and rotates the password session", async () => {
    navigation.pathname = "/account";
    window.history.replaceState(null, "", "/account");
    const fetchMock = vi.fn(async () => new Response(JSON.stringify(registered), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);
    wrapped(<AccountSettings />, registered);
    expect(screen.getByText("@alice")).toBeInTheDocument();
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.queryByRole("textbox", { name: /用户名|显示名称/ })).not.toBeInTheDocument();
    const user = userEvent.setup();
    await user.type(screen.getByLabelText("当前密码", { selector: "input" }), "current password");
    await user.type(screen.getByLabelText("新密码", { selector: "input" }), "a different sufficiently long password");
    await user.type(screen.getByLabelText("确认密码", { selector: "input" }), "a different sufficiently long password");
    await user.click(screen.getByRole("button", { name: "更新密码" }));
    expect(await screen.findByText("密码已更新，其他设备已退出登录")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/auth/change-password", expect.objectContaining({
      method: "POST", body: expect.stringContaining('"currentPassword":"current password"')
    }));
  });
});
