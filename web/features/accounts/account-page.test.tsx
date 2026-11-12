import { act, cleanup, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { render } from "@/components/toast-test-utils";
import { api } from "@/lib/api/client";
import { AccountPage } from "./account-page";

const { refresh } = vi.hoisted(() => ({ refresh: vi.fn(async () => {}) }));
vi.mock("@/features/auth/auth-provider", () => ({
  useAuth: () => ({ refresh, context: { user: { username: "account-user", displayName: "Account user", role: "user" } } }),
}));
vi.mock("./browser-storage-settings", () => ({ BrowserStorageSettings: () => null }));
afterEach(() => { cleanup(); vi.restoreAllMocks(); refresh.mockClear(); });

function openPassword() {
  fireEvent.click(screen.getByRole("button", { name: "修改密码" }));
  const dialog = screen.getByRole("dialog", { name: "修改密码" });
  fireEvent.change(within(dialog).getByLabelText("当前密码", { exact: true }), { target: { value: "original-password" } });
  fireEvent.change(within(dialog).getByLabelText("新密码", { exact: true }), { target: { value: "replacement-password" } });
  fireEvent.change(within(dialog).getByLabelText("确认密码", { exact: true }), { target: { value: "replacement-password" } });
  return dialog;
}

test("opens password editing on demand and discards fields and errors on cancel", async () => {
  const post = vi.spyOn(api, "POST");
  render(<AccountPage />);
  expect(screen.queryByLabelText("当前密码", { exact: true })).not.toBeInTheDocument();
  const trigger = screen.getByRole("button", { name: "修改密码" });
  trigger.focus();
  const dialog = openPassword();
  fireEvent.change(within(dialog).getByLabelText("确认密码", { exact: true }), { target: { value: "different-password" } });
  fireEvent.click(within(dialog).getByRole("button", { name: "更新密码" }));
  expect(await screen.findByText("两次输入的新密码不一致。")).toBeVisible();
  expect(post).not.toHaveBeenCalled();
  fireEvent.keyDown(document.activeElement ?? dialog, { key: "Escape" });
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(trigger).toHaveFocus();
  fireEvent.click(trigger);
  expect(screen.getByLabelText("当前密码", { exact: true })).toHaveValue("");
  expect(screen.getByLabelText("新密码", { exact: true })).toHaveValue("");
  expect(screen.queryByText("两次输入的新密码不一致。")).not.toBeInTheDocument();
});

test("keeps rejected password values available for retry and closes only after success", async () => {
  const post = vi.spyOn(api, "POST").mockResolvedValueOnce({
    error: { code: "INVALID_PASSWORD", message: "当前密码不正确。" },
    response: new Response(null, { status: 400 }),
  });
  render(<AccountPage />);
  const dialog = openPassword();
  fireEvent.click(within(dialog).getByRole("button", { name: "更新密码" }));
  expect(await screen.findByText("当前密码不正确。")).toBeVisible();
  expect(within(dialog).getByLabelText("新密码", { exact: true })).toHaveValue("replacement-password");
  post.mockResolvedValueOnce({ response: new Response(null, { status: 204 }) });
  fireEvent.click(within(dialog).getByRole("button", { name: "更新密码" }));
  expect(await screen.findByText("密码已更新，其他设备已退出登录。")).toBeVisible();
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(refresh).toHaveBeenCalledOnce();
  expect(post).toHaveBeenLastCalledWith("/api/v1/auth/change-password", {
    body: { currentPassword: "original-password", newPassword: "replacement-password" },
  });
});

test("prevents closing or submitting twice while the password request is pending", async () => {
  let finish!: () => void;
  const post = vi.spyOn(api, "POST").mockImplementationOnce(() => new Promise(resolve => {
    finish = () => resolve({ response: new Response(null, { status: 204 }) });
  }));
  render(<AccountPage />);
  const dialog = openPassword();
  fireEvent.click(within(dialog).getByRole("button", { name: "更新密码" }));
  expect(within(dialog).getByRole("button", { name: "处理中…" })).toBeDisabled();
  expect(within(dialog).getByRole("button", { name: "取消" })).toBeDisabled();
  fireEvent.keyDown(dialog, { key: "Escape" });
  expect(dialog).toBeVisible();
  expect(post).toHaveBeenCalledOnce();
  await act(async () => finish());
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
