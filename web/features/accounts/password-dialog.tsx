"use client";
import { useId, useRef, useState, type FormEvent } from "react";
import { api, ApiError } from "@/lib/api/client";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { FeedbackBanner } from "@/components/ui";
import { useToast } from "@/components/toast-provider";
import { useAuth } from "@/features/auth/auth-provider";

export function PasswordDialog({ onClose }: { onClose: () => void }) {
  const { notify } = useToast();
  const { refresh } = useAuth();
  const formRef = useRef<HTMLFormElement>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busy) { return; }
    setError("");
    const form = new FormData(event.currentTarget);
    const newPassword = String(form.get("newPassword"));
    if (newPassword !== form.get("confirmation")) {
      setError("两次输入的新密码不一致。");
      return;
    }
    setBusy(true);
    try {
      const response = await api.POST("/api/v1/auth/change-password", {
        body: { currentPassword: String(form.get("currentPassword")), newPassword },
      });
      if (response.error) {
        if (response.response.status === 400) {
          setError(response.error.message);
          return;
        }
        throw new ApiError(response.error.code, response.error.message, response.response.status);
      }
      await refresh();
      onClose();
      notify({ tone: "good", message: "密码已更新，其他设备已退出登录。" });
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "密码更新失败。" });
    } finally { setBusy(false); }
  }
  return <ConfirmDialog open portalToBody role="dialog" title="修改密码"
    description="更新后，其他设备上的会话将立即退出。"
    confirmLabel="更新密码" busy={busy} onCancel={onClose}
    onConfirm={() => formRef.current?.requestSubmit()}>
    <form ref={formRef} className="auth-form account-password-form" aria-busy={busy}
      onSubmit={(event) => void submit(event)}>
      <PasswordField label="当前密码" name="currentPassword" current />
      <PasswordField label="新密码" name="newPassword" />
      <PasswordField label="确认密码" name="confirmation" />
      <p className="password-policy">至少 12 个字符，可以使用空格；不要使用常见或已泄露的密码。</p>
      {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
      <button type="submit" hidden disabled={busy}>更新密码</button>
    </form>
  </ConfirmDialog>;
}

function PasswordField({ label, name, current = false }: { label: string; name: string; current?: boolean }) {
  const id = useId();
  const [visible, setVisible] = useState(false);
  return <div className="form-field">
    <label htmlFor={id}>{label}</label>
    <div className="password-control">
      <input id={id} name={name} type={visible ? "text" : "password"}
        autoComplete={current ? "current-password" : "new-password"} minLength={current ? undefined : 12} required />
      <button type="button" aria-label={`${visible ? "隐藏" : "显示"}${label}`} aria-pressed={visible}
        onClick={() => setVisible(!visible)}>{visible ? "隐藏" : "显示"}</button>
    </div>
  </div>;
}
