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
  let reject!: (failure: Error) => void;
  post.mockImplementationOnce(() => new Promise((_, rejectRequest) => {
    reject = rejectRequest;
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

  await act(async () => reject(new Error("用户名或密码不正确")));
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
