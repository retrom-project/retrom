"use client";

import { useCallback, useLayoutEffect, useRef, useState, type FormEvent } from "react";
import { useToast } from "@/components/toast-provider";
import type { readAPIError } from "./types";

export type PasswordFieldName = "currentPassword" | "password" | "passwordConfirmation" | "newPassword" | "newPasswordConfirmation";
export type SubmitState = {
  busy: boolean;
  error: string | null;
  requestId?: string;
  invalidField?: PasswordFieldName | "credentials";
  clearFields?: readonly string[];
};
export const initialSubmit: SubmitState = { busy: false, error: null };

type APIIssue = Awaited<ReturnType<typeof readAPIError>>;
const passwordMessages: Record<string, string> = {
  TOO_SHORT: "密码至少需要 6 个字符",
  TOO_LONG: "密码不能超过 128 个字符或 512 字节",
  CONTROL_CHARACTER: "密码不能包含控制字符",
  COMMON_PASSWORD: "这个密码过于常见，请换一个密码",
  CONTEXT_PASSWORD: "密码不能与用户名、显示名称或 Retrom 相同",
};

export function passwordErrorState(issue: APIIssue, form: "create" | "change"): SubmitState {
  const state: SubmitState = { busy: false, error: issue.message, requestId: issue.requestId };
  if (issue.code === "CURRENT_PASSWORD_INVALID") {
    return { ...state, error: "当前密码不正确", invalidField: "currentPassword", clearFields: ["currentPassword"] };
  }
  if (issue.code !== "PASSWORD_POLICY_VIOLATION") {return state;}
  const password = form === "change" ? "newPassword" : "password";
  const confirmation = form === "change" ? "newPasswordConfirmation" : "passwordConfirmation";
  if (issue.reasonCode === "CONFIRMATION_MISMATCH") {
    return { ...state, error: "确认密码与新密码不一致", invalidField: confirmation, clearFields: [password, confirmation] };
  }
  const message = issue.reasonCode ? passwordMessages[issue.reasonCode] : undefined;
  return message ? { ...state, error: message, invalidField: password, clearFields: [password] } : state;
}

export function useSubmitState() {
  const [state, setState] = useState(initialSubmit);
  const formRef = useRef<HTMLFormElement>(null);
  const { notify } = useToast();
  const update = useCallback((next: SubmitState) => {
    setState(next);
    if (next.error) {notify({ tone: "bad", message: `${next.error}${next.requestId ? `（请求 ID：${next.requestId}）` : ""}` });}
  }, [notify]);
  useLayoutEffect(() => {
    if (state.busy || !state.error) {return;}
    const form = formRef.current;
    const name = state.invalidField === "credentials" ? "password" : state.invalidField;
    const field = name ? form?.elements.namedItem(name) : null;
    if (field instanceof HTMLInputElement) {field.focus();}
    else {form?.querySelector<HTMLButtonElement>('button[type="submit"]')?.focus();}
  }, [state.busy, state.error, state.invalidField]);
  function onInput(event: FormEvent<HTMLFormElement>) {
    const input = event.target;
    if (input instanceof HTMLInputElement && state.clearFields?.includes(input.name)) {
      setState(initialSubmit);
    }
  }
  return [state, update, { ref: formRef, onInput }] as const;
}
