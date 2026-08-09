import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthProvider } from "./auth-provider";
import { LoginForm, RegisterForm } from "./auth-ui";
import type { AuthContext } from "./types";

const navigation = vi.hoisted(() => ({ pathname: "/login", replace: vi.fn(), refresh: vi.fn() }));
vi.mock("next/navigation", () => ({ usePathname: () => navigation.pathname, useRouter: () => navigation }));

const anonymous: AuthContext = {
  instanceState: "READY", mode: "test", authenticationState: "UNAUTHENTICATED", user: null, csrfToken: null,
  idleExpiresAtMs: null, absoluteExpiresAtMs: null, testDefaultAccountActive: true
};
const registered: AuthContext = {
  ...anonymous, authenticationState: "AUTHENTICATED", csrfToken: "csrf", user: { userId: "user-1", username: "alice", displayName: "Alice", role: "USER" }
};

function wrapped(node: React.ReactNode) {
  return render(<AuthProvider initialContext={anonymous}>{node}</AuthProvider>);
}

beforeEach(() => {
  navigation.replace.mockReset(); navigation.refresh.mockReset(); navigation.pathname = "/login";
  window.history.replaceState(null, "", "/login");
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("authentication UI", () => {
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
});
