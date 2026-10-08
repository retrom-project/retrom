"use client";
import Link from "next/link";
import { AccountLinkStatus, useAccountLinkToken } from "./account-link-status";
import { useState } from "react";
import type { FormEvent } from "react";
import { api, result } from "@/lib/api/client";
import { useAuth } from "./auth-provider";
import { FeedbackBanner } from "@/components/ui";
import { useToast } from "@/components/toast-provider";
import { LoginForm } from "./login-form";
export function AuthForm({
  mode,
}: {
  mode: "login" | "setup" | "register" | "reset-password";
}) {
  const token = useAccountLinkToken();
  const { accept } = useAuth();
  const { notify, clear } = useToast();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [complete, setComplete] = useState(false);
  const login = mode === "login";
  const title = {
    login: "登录",
    setup: "初始化 Retrom",
    register: "接受邀请",
    "reset-password": "重置密码",
  }[mode];
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (login) {
      clear();
    }
    setBusy(true);
    setError("");
    const data = new FormData(event.currentTarget);
    const username = String(data.get("username") ?? "");
    const password = String(data.get("password") ?? "");
    const displayName = String(data.get("displayName") ?? "");
    try {
      if (mode === "login") {
        accept(
          result(
            await api.POST("/api/v1/auth/login", {
              body: { username, password },
            }),
          ),
        );
      } else if (mode === "setup") {
        accept(
          result(
            await api.POST("/api/v1/auth/initialize", {
              body: { username, password, displayName },
            }),
          ),
        );
      } else if (mode === "register") {
        accept(
          result(
            await api.POST("/api/v1/auth/invitations/accept", {
              body: { username, password, displayName, token },
            }),
          ),
        );
      } else {
        const response = await api.POST(
          "/api/v1/auth/password-resets/complete",
          { body: { token, newPassword: password } },
        );
        if (response.error) {
          throw new Error(response.error.message);
        }
        setComplete(true);
      }
    } catch (failure) {
      const message = failure instanceof Error ? failure.message : "请求失败。";
      setError(message);
      if (login) {
        notify({ tone: "bad", message });
      }
    } finally {
      setBusy(false);
    }
  }
  if (login) {
    return <LoginForm busy={busy} error={error} onSubmit={submit} />;
  }
  return (
    <main className="auth-page">
      <section className="auth-card">
        <div className="brand-mark">R</div>
        <h1>{title}</h1>
        <p>自托管复古游戏资料库</p>
        {mode === "register" || mode === "reset-password" ? (
          <AccountLinkStatus token={token} />
        ) : null}
        {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
        {complete ? (
          <FeedbackBanner tone="good">
            密码已更新。<Link href="/login">前往登录</Link>
          </FeedbackBanner>
        ) : (
          <form
            className="stack"
            onSubmit={(event) => void submit(event)}
            aria-busy={busy}
          >
            {mode !== "reset-password" ? (
              <label className="field">
                账号
                <input name="username" autoComplete="username" required />
              </label>
            ) : null}
            {mode === "setup" || mode === "register" ? (
              <label className="field">
                显示名称
                <input name="displayName" autoComplete="nickname" required />
              </label>
            ) : null}
            <label className="field">
              密码
              <input
                type="password"
                name="password"
                autoComplete="new-password"
                minLength={12}
                required
              />
            </label>
            <button className="button" disabled={busy}>
              {busy ? "正在提交…" : title}
            </button>
          </form>
        )}
      </section>
    </main>
  );
}
