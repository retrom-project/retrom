import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { render } from "@/components/toast-test-utils";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { AuthProvider } from "./auth-provider";
import { AccountSettings } from "./auth-ui";
import type { AuthContext } from "./types";

vi.mock("next/navigation", () => ({ usePathname: () => "/account", useRouter: () => ({ replace: vi.fn(), refresh: vi.fn() }) }));
const account: AuthContext = { instanceState: "READY", mode: "test", authenticationState: "AUTHENTICATED", csrfToken: "csrf", user: { userId: "user", username: "alice", displayName: "Alice", role: "USER" }, idleExpiresAtMs: null, absoluteExpiresAtMs: null, testDefaultAccountActive: false };
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.useRealTimers(); });

it("associates confirmation mismatch with the editable field and restores keyboard focus", async () => {
  const fetch = vi.fn(async () => new Response(JSON.stringify({ error: { code: "PASSWORD_POLICY_VIOLATION", message: "密码不符合要求", details: { reasonCode: "CONFIRMATION_MISMATCH" } } }), { status: 422 }));
  vi.stubGlobal("fetch", fetch);
  render(<AuthProvider initialContext={account}><AccountSettings /></AuthProvider>);
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("当前密码", { selector: "input" }), "current secret");
  await user.type(screen.getByLabelText("新密码", { selector: "input" }), "new secret");
  const confirmation = screen.getByLabelText("确认密码", { selector: "input" });
  await user.type(confirmation, "different secret");
  screen.getByRole("button", { name: "更新密码" }).focus();
  await user.keyboard("{Enter}");
  expect(await screen.findByRole("alert")).toHaveTextContent("确认密码与新密码不一致");
  expect(confirmation).toHaveAttribute("aria-invalid", "true");
  expect(confirmation).toHaveAccessibleDescription("确认密码与新密码不一致");
  expect(confirmation).toHaveFocus();
  await user.type(confirmation, " correction");
  expect(confirmation).toHaveAttribute("aria-invalid", "false");
  fetch.mockImplementation(async () => new Response(JSON.stringify(account), { status: 200 }));
  await user.click(screen.getByRole("button", { name: "更新密码" }));
  await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("密码已更新，其他设备已退出登录"));
});

it.each([
  ["TOO_SHORT", "密码至少需要 6 个字符"],
  ["TOO_LONG", "密码不能超过 128 个字符或 512 字节"],
  ["CONTROL_CHARACTER", "密码不能包含控制字符"],
  ["COMMON_PASSWORD", "这个密码过于常见，请换一个密码"],
  ["CONTEXT_PASSWORD", "密码不能与用户名、显示名称或 Retrom 相同"],
])("maps %s to the new password without marking the other fields", async (reason, message) => {
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: { code: "PASSWORD_POLICY_VIOLATION", message: "密码不符合要求", details: { reasonCode: reason } } }), { status: 422 })));
  render(<AuthProvider initialContext={account}><AccountSettings /></AuthProvider>);
  const user = userEvent.setup();
  for (const label of ["当前密码", "新密码", "确认密码"]) {
    await user.type(screen.getByLabelText(label, { selector: "input" }), "valid length phrase");
  }
  await user.click(screen.getByRole("button", { name: "更新密码" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(message);
  const password = screen.getByLabelText("新密码", { selector: "input" });
  expect(password).toHaveFocus();
  expect(password).toHaveAccessibleDescription(message);
  expect(screen.getByLabelText("当前密码", { selector: "input" })).toHaveAttribute("aria-invalid", "false");
  expect(screen.getByLabelText("确认密码", { selector: "input" })).toHaveAttribute("aria-invalid", "false");
});

it("retains field semantics after toast dismissal and clears mismatch when either password changes", async () => {
  vi.useFakeTimers();
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ error: { code: "PASSWORD_POLICY_VIOLATION", message: "密码不符合要求", details: { reasonCode: "CONFIRMATION_MISMATCH" } } }), { status: 422 })));
  render(<AuthProvider initialContext={account}><AccountSettings /></AuthProvider>);
  const form = screen.getByRole("button", { name: "更新密码" }).closest("form");
  if (!form) {throw new Error("password form missing");}
  await act(async () => { fireEvent.submit(form); });
  expect(screen.getByRole("alert")).toHaveTextContent("确认密码与新密码不一致");
  await act(async () => { await vi.advanceTimersByTimeAsync(3001); });
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  const confirmation = screen.getByLabelText("确认密码", { selector: "input" });
  expect(confirmation).toHaveAccessibleDescription("确认密码与新密码不一致");
  fireEvent.input(screen.getByLabelText("新密码", { selector: "input" }), { target: { value: "corrected secret" } });
  expect(confirmation).toHaveAttribute("aria-invalid", "false");
  expect(confirmation).not.toHaveAttribute("aria-describedby");
});
