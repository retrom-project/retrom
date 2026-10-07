"use client";
import { useState } from "react";
import type { FormEvent } from "react";
import { api } from "@/lib/api/client";
import { useAuth } from "@/features/auth/auth-provider";
import { PageHeader, FeedbackBanner } from "@/components/ui";
export function AccountPage() {
  const { context } = useAuth();
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    const form = new FormData(event.currentTarget);
    const newPassword = String(form.get("newPassword"));
    if (newPassword !== form.get("confirmation")) {
      setError("两次输入的新密码不一致。");
      setBusy(false);
      return;
    }
    const response = await api.POST("/api/v1/auth/change-password", {
      body: {
        currentPassword: String(form.get("currentPassword")),
        newPassword,
      },
    });
    setBusy(false);
    if (response.error) {
      setError(response.error.message);
      return;
    }
    setError("");
    setMessage("密码已更新。其他登录会话已按账号安全规则处理。");
  }
  return (
    <>
      <PageHeader title="账户设置" description="管理账号密码与查看当前身份。" />
      <section className="workspace-card">
        <h2>{context?.user?.displayName}</h2>
        <p>
          @{context?.user?.username} ·{" "}
          {context?.user?.role === "admin" ? "管理员" : "普通用户"}
        </p>
      </section>
      <form
        className="workspace-section workspace-card stack"
        onSubmit={(event) => void submit(event)}
      >
        <h2>修改密码</h2>
        <label className="field">
          当前密码
          <input
            name="currentPassword"
            type="password"
            autoComplete="current-password"
            required
          />
        </label>
        <label className="field">
          新密码
          <input
            name="newPassword"
            type="password"
            autoComplete="new-password"
            minLength={12}
            required
          />
        </label>
        <label className="field">
          确认新密码
          <input
            name="confirmation"
            type="password"
            autoComplete="new-password"
            minLength={12}
            required
          />
        </label>
        {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
        {message ? (
          <FeedbackBanner tone="good">{message}</FeedbackBanner>
        ) : null}
        <button className="button" disabled={busy}>
          更新密码
        </button>
      </form>
    </>
  );
}
