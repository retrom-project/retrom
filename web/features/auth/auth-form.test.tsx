import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ToastProvider } from "@/components/toast-provider";
import { api } from "@/lib/api/client";
import { AuthForm } from "./auth-form";

const { accept } = vi.hoisted(() => ({ accept: vi.fn() }));
vi.mock("./auth-provider", () => ({ useAuth: () => ({ accept }) }));

afterEach(() => { cleanup(); vi.restoreAllMocks(); accept.mockReset(); });

it("reports login failure once, releases the submit control, and accepts a later retry", async () => {
  const post = vi.spyOn(api, "POST");
  const failedResponse = {
    error: { code: "AUTHENTICATION_REQUIRED", message: "Sign in to continue" },
    response: new Response(null, { status: 401 }),
  };
  let respond!: (response: typeof failedResponse) => void;
  post.mockImplementationOnce(() => new Promise((resolve) => {
    respond = resolve;
  }));
  render(<ToastProvider><AuthForm mode="login" /></ToastProvider>);
  const username = screen.getByLabelText("用户名");
  const password = screen.getByLabelText("密码");
  fireEvent.change(username, { target: { value: "review-user" } });
  fireEvent.change(password, { target: { value: "entered-password" } });
  const submit = screen.getByRole("button", { name: "登录" });
  submit.focus();
  fireEvent.click(submit);
  expect(submit).toBeDisabled();
  expect(submit.closest("form")).toHaveAttribute("aria-busy", "true");
  fireEvent.click(submit);
  expect(post).toHaveBeenCalledTimes(1);

  await act(async () => respond(failedResponse));
  expect(accept).not.toHaveBeenCalled();
  expect(submit).toBeEnabled();
  expect(submit.closest("form")).toHaveAttribute("aria-busy", "false");
  expect(submit.closest("form")).toHaveAccessibleDescription("用户名或密码不正确");
  expect(screen.getAllByRole("alert")).toHaveLength(1);
  expect(screen.getByRole("alert")).toHaveClass("app-toast");
  expect(screen.getByRole("alert")).toHaveAttribute("aria-live", "assertive");
  const context = { initialized: true, user: null, csrfToken: "retry-csrf" };
  post.mockResolvedValueOnce({ data: context, response: new Response() });
  fireEvent.change(password, { target: { value: "retry-password" } });
  await act(async () => fireEvent.click(submit));
  expect(post).toHaveBeenLastCalledWith("/api/v1/auth/login", {
    body: { username: "review-user", password: "retry-password" },
  });
  expect(accept).toHaveBeenCalledExactlyOnceWith(context);
  expect(submit).toBeEnabled();
  expect(screen.queryByRole("alert")).toBeNull();
  expect(submit.closest("form")).not.toHaveAttribute("aria-describedby");
});

it.each([
  { mode: "login", status: 429, code: "RATE_LIMITED", message: "Try again later" },
  { mode: "login", status: 401, code: "OTHER_AUTH_ERROR", message: "Other authentication error" },
  { mode: "login", status: 500, code: "AUTHENTICATION_REQUIRED", message: "Unexpected server error" },
  { mode: "setup", status: 401, code: "AUTHENTICATION_REQUIRED", message: "Sign in to continue" },
] as const)("preserves $mode $status/$code errors", async ({ mode, status, code, message }) => {
  vi.spyOn(api, "POST").mockResolvedValueOnce({
    error: { code, message },
    response: new Response(null, { status }),
  });
  render(<ToastProvider><AuthForm mode={mode} /></ToastProvider>);
  fireEvent.change(screen.getByLabelText(mode === "login" ? "用户名" : "账号"), {
    target: { value: "review-user" },
  });
  fireEvent.change(screen.getByLabelText("密码"), { target: { value: "entered-password" } });
  if (mode === "setup") {
    fireEvent.change(screen.getByLabelText("显示名称"), { target: { value: "Review User" } });
  }
  const submit = screen.getByRole("button", { name: mode === "login" ? "登录" : "初始化 Retrom" });
  await act(async () => fireEvent.click(submit));
  expect(accept).not.toHaveBeenCalled();
  expect(submit).toBeEnabled();
  expect(screen.getAllByRole("alert")).toHaveLength(1);
  expect(screen.getByRole("alert")).toHaveTextContent(message);
  expect(screen.getByRole("alert")).toHaveClass(mode === "login" ? "app-toast" : "feedback-banner");
});
