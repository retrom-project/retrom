import { cleanup, fireEvent, screen } from "@testing-library/react";
import { render } from "@/components/toast-test-utils";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "./auth-provider";
import { RegisterForm, ResetPasswordForm, SetupForm } from "./auth-ui";
import type { AuthContext } from "./types";

const navigation = vi.hoisted(() => ({ pathname: "/setup", replace: vi.fn(), refresh: vi.fn() }));
vi.mock("next/navigation", () => ({ usePathname: () => navigation.pathname, useRouter: () => navigation }));
const anonymous: AuthContext = { instanceState: "READY", mode: "release", authenticationState: "UNAUTHENTICATED", csrfToken: null, user: null, idleExpiresAtMs: null, absoluteExpiresAtMs: null, testDefaultAccountActive: false };
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it.each([
  { path: "/setup", hash: "", Form: SetupForm, button: "创建管理员并进入 Retrom", kind: "INVITATION" },
  { path: "/register", hash: "#invite=fixture", Form: RegisterForm, button: "创建账号并进入 Retrom", kind: "INVITATION" },
  { path: "/reset-password", hash: "#reset=fixture", Form: ResetPasswordForm, button: "更新密码", kind: "PASSWORD_RESET" },
])("restores the confirmation field for $path", async ({ path, hash, Form, button, kind }) => {
  navigation.pathname = path;
  window.history.replaceState(null, "", path + hash);
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => String(input).endsWith("/inspect")
    ? new Response(JSON.stringify({ kind, role: "USER", username: "alice", expiresAtMs: 1_900_000_000_000 }), { status: 200 })
    : new Response(JSON.stringify({ error: { code: "PASSWORD_POLICY_VIOLATION", message: "密码不符合要求", details: { reasonCode: "CONFIRMATION_MISMATCH" } } }), { status: 422 })));
  const context: AuthContext = path === "/setup" ? { ...anonymous, instanceState: "INITIALIZATION_REQUIRED", authenticationState: "NOT_APPLICABLE" } : anonymous;
  render(<AuthProvider initialContext={context}><Form /></AuthProvider>);
  const form = (await screen.findByRole("button", { name: button })).closest("form");
  if (!form) {throw new Error("password form missing");}
  fireEvent.submit(form);
  expect(await screen.findByRole("alert")).toHaveTextContent("确认密码与新密码不一致");
  const confirmation = screen.getByLabelText("确认密码", { selector: "input" });
  expect(confirmation).toHaveAttribute("aria-invalid", "true");
  expect(confirmation).toHaveAccessibleDescription("确认密码与新密码不一致");
  expect(confirmation).toHaveFocus();
});
