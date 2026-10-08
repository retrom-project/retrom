import type { FormEvent } from "react";

export function LoginForm({ busy, error, onSubmit }: {
  busy: boolean;
  error: string;
  onSubmit: (event: FormEvent<HTMLFormElement>) => Promise<void>;
}) {
  return (
    <main className="auth-canvas">
      <section className="auth-panel">
        <div className="auth-brand">
          <span className="brand-mark" aria-hidden="true">R</span>
          <strong>Retrom</strong>
        </div>
        <p className="eyebrow">欢迎回来</p>
        <h1>登录</h1>
        {error ? <span id="login-error" className="sr-only">{error}</span> : null}
        <form
          className="auth-form"
          onSubmit={(event) => void onSubmit(event)}
          aria-busy={busy}
          aria-describedby={error ? "login-error" : undefined}
        >
          <label className="form-field">
            <span className="form-field-label">用户名</span>
            <input name="username" autoComplete="username" required />
          </label>
          <label className="form-field">
            <span className="form-field-label">密码</span>
            <input
              type="password"
              name="password"
              autoComplete="current-password"
              minLength={1}
              required
            />
          </label>
          <button className="button auth-submit" disabled={busy}>
            {busy ? "正在提交…" : "登录"}
          </button>
        </form>
        <p className="auth-footnote">
          新账号需要管理员邀请。本阶段不提供自助找回密码。
        </p>
      </section>
    </main>
  );
}
