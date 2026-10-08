"use client";
import { useId, useState } from "react";
import type { FormEvent } from "react";
import { api, ApiError } from "@/lib/api/client";
import { useAuth } from "@/features/auth/auth-provider";
import { PageHeader, FeedbackBanner, StatusBadge } from "@/components/ui";
import { useToast } from "@/components/toast-provider";
export function AccountPage() {
  const { context } = useAuth();
  const { notify } = useToast();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const element = event.currentTarget;
    setError("");
    setBusy(true);
    const form = new FormData(event.currentTarget);
    const newPassword = String(form.get("newPassword"));
    if (newPassword !== form.get("confirmation")) {
      setError("两次输入的新密码不一致。");
      setBusy(false);
      return;
    }
    try {
      const response = await api.POST("/api/v1/auth/change-password", {
        body: {
          currentPassword: String(form.get("currentPassword")),
          newPassword,
        },
      });
      if (response.error) {
        if (response.response.status === 400) {
          setError(response.error.message);
          return;
        }
        throw new ApiError(response.error.code, response.error.message, response.response.status);
      }
      element.reset();
      notify({ tone: "good", message: "密码已更新，其他设备已退出登录。" });
    } catch (failure) {
      notify({
        tone: "bad",
        message: failure instanceof Error ? failure.message : "密码更新失败。",
      });
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHeader
        title="账户设置"
        description="查看当前账号资料并更新登录密码。"
      />
      <div className="account-settings-grid">
        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>账号资料</h2>
              <p>本版本不提供自行修改用户名或显示名称。</p>
            </div>
          </div>
          <dl className="account-facts">
            <div>
              <dt>用户名</dt>
              <dd>@{context?.user?.username}</dd>
            </div>
            <div>
              <dt>显示名称</dt>
              <dd>{context?.user?.displayName}</dd>
            </div>
            <div>
              <dt>角色</dt>
              <dd>
                <StatusBadge
                  tone={context?.user?.role === "admin" ? "info" : "neutral"}
                >
                  {context?.user?.role === "admin" ? "管理员" : "普通用户"}
                </StatusBadge>
              </dd>
            </div>
            <div>
              <dt>账号状态</dt>
              <dd>
                <StatusBadge tone="good">启用</StatusBadge>
              </dd>
            </div>
          </dl>
        </section>
        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>修改密码</h2>
              <p>更新后，其他设备上的会话将立即退出。</p>
            </div>
          </div>
          <div className="panel-body">
            <form
              className="auth-form account-password-form"
              aria-busy={busy}
              onSubmit={(event) => void submit(event)}
            >
              <PasswordField label="当前密码" name="currentPassword" current />
              <PasswordField label="新密码" name="newPassword" />
              <PasswordField label="确认密码" name="confirmation" />
              <p className="password-policy">
                至少 12 个字符，可以使用空格；不要使用常见或已泄露的密码。
              </p>
              {error ? (
                <FeedbackBanner tone="bad">{error}</FeedbackBanner>
              ) : null}
              <button className="button" disabled={busy}>
                {busy ? "正在更新…" : "更新密码"}
              </button>
            </form>
          </div>
        </section>
      </div>
    </>
  );
}

function PasswordField({
  label,
  name,
  current = false,
}: {
  label: string;
  name: string;
  current?: boolean;
}) {
  const id = useId();
  const [visible, setVisible] = useState(false);
  return (
    <div className="form-field">
      <label htmlFor={id}>{label}</label>
      <div className="password-control">
        <input
          id={id}
          name={name}
          type={visible ? "text" : "password"}
          autoComplete={current ? "current-password" : "new-password"}
          minLength={current ? undefined : 12}
          required
        />
        <button
          type="button"
          aria-label={`${visible ? "隐藏" : "显示"}${label}`}
          aria-pressed={visible}
          onClick={() => setVisible(!visible)}
        >
          {visible ? "隐藏" : "显示"}
        </button>
      </div>
    </div>
  );
}
