"use client";

import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { FeedbackBanner, StatusBadge } from "@/components/ui";
import { safeReturnTo, useAuth } from "./auth-provider";
import { readAPIError, type AuthContext } from "./types";
import type { components } from "@/lib/api/generated/schema";

const minimumPasswordCharacters = 6;

export function AuthPanel({ title, eyebrow, description, width = "narrow", children }: { title: string; eyebrow: string; description?: string; width?: "narrow" | "wide"; children: ReactNode }) {
  return <main className="auth-canvas"><section className={`auth-panel ${width === "wide" ? "is-wide" : ""}`}>
    <div className="auth-brand"><span className="brand-mark" aria-hidden="true">R</span><strong>Retrom</strong></div>
    <p className="eyebrow">{eyebrow}</p><h1 tabIndex={-1}>{title}</h1>{description ? <p className="auth-description">{description}</p> : null}
    {children}
  </section></main>;
}

function PasswordField({ name, label, autoComplete, required = true }: { name: string; label: string; autoComplete: string; required?: boolean }) {
  const [visible, setVisible] = useState(false);
  return <div className="form-field"><label htmlFor={name}>{label}</label><div className="password-control">
    <input autoComplete={autoComplete} id={name} minLength={autoComplete === "new-password" ? minimumPasswordCharacters : undefined} name={name} required={required} type={visible ? "text" : "password"} />
    <button aria-label={visible ? `隐藏${label}` : `显示${label}`} type="button" onClick={() => setVisible((value) => !value)}>{visible ? "隐藏" : "显示"}</button>
  </div></div>;
}

function PasswordPolicy() {
  return <p className="password-policy">至少 6 个字符，可以使用空格；不要求特定字符组合。</p>;
}

function ErrorSummary({ message, requestId, errorRef }: { message: string | null; requestId?: string; errorRef: React.RefObject<HTMLDivElement | null> }) {
  useEffect(() => { if (message) {errorRef.current?.focus();} }, [errorRef, message]);
  return message ? <div className="form-error" role="alert" tabIndex={-1} ref={errorRef}><strong>{message}</strong>{requestId ? <small>请求 ID：{requestId}</small> : null}</div> : null;
}

type SubmitState = { busy: boolean; error: string | null; requestId?: string };
const initialSubmit: SubmitState = { busy: false, error: null };

export function SetupForm() {
  const { acceptContext } = useAuth();
  const router = useRouter();
  const [state, setState] = useState(initialSubmit);
  const errorRef = useRef<HTMLDivElement>(null);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setState({ busy: true, error: null });
    const form = event.currentTarget;
    const values = new FormData(form);
    const response = await fetch("/api/v1/auth/initialize", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify({
      setupCode: values.get("setupCode"), username: values.get("username"), displayName: values.get("displayName"), password: values.get("password"), passwordConfirmation: values.get("passwordConfirmation")
    }) }).catch(() => null);
    if (!response) { setState({ busy: false, error: "无法连接服务器，请检查网络后重试" }); return; }
    if (!response.ok) {
      const issue = await readAPIError(response, "初始化失败，请重试");
      if (["INITIALIZATION_ALREADY_COMPLETED", "INSTANCE_ALREADY_INITIALIZED"].includes(issue.code)) {
        form.reset(); router.replace("/login"); return;
      }
      setState({ busy: false, error: issue.code === "INITIALIZATION_PROOF_INVALID" ? "初始化码无效" : issue.message, requestId: issue.requestId }); return;
    }
    acceptContext(await response.json() as AuthContext); router.replace("/"); router.refresh();
  }
  return <AuthPanel eyebrow="首次设置" title="创建首位管理员" description="创建此服务器的首位管理员。初始化完成后，其他账号只能通过邀请创建。" width="wide">
    <form className="auth-form" onSubmit={(event) => void submit(event)} aria-busy={state.busy}>
      <ErrorSummary message={state.error} requestId={state.requestId} errorRef={errorRef} />
      <PasswordField autoComplete="off" label="初始化码" name="setupCode" />
      <p className="field-help">在服务器主机执行 <code>retrom setup-code</code> 获取；命令结果不会在浏览器显示。</p>
      <div className="form-field"><label htmlFor="username">管理员用户名</label><input autoComplete="username" id="username" name="username" required /></div>
      <div className="form-field"><label htmlFor="displayName">显示名称</label><input autoComplete="name" id="displayName" name="displayName" required /></div>
      <PasswordField autoComplete="new-password" label="密码" name="password" />
      <PasswordField autoComplete="new-password" label="确认密码" name="passwordConfirmation" />
      <PasswordPolicy />
      <button className="button auth-submit" disabled={state.busy} type="submit">{state.busy ? "正在初始化…" : "创建管理员并进入 Retrom"}</button>
    </form>
  </AuthPanel>;
}

export function LoginForm() {
  const { context, acceptContext } = useAuth();
  const router = useRouter();
  const [state, setState] = useState(initialSubmit);
  const [retryAt, setRetryAt] = useState<number | null>(null);
  const [rateLimited, setRateLimited] = useState(false);
  const errorRef = useRef<HTMLDivElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (!retryAt) {return;}
    const timer = window.setInterval(() => { if (Date.now() >= retryAt) { setRetryAt(null); setRateLimited(false); setState(initialSubmit); } }, 1000);
    return () => window.clearInterval(timer);
  }, [retryAt]);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (retryAt && Date.now() < retryAt) {return;}
    setState({ busy: true, error: null }); const values = new FormData(event.currentTarget);
    const response = await fetch("/api/v1/auth/login", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ username: values.get("username"), password: values.get("password") }) }).catch(() => null);
    if (!response) { setState({ busy: false, error: "无法连接服务器，请检查网络后重试" }); return; }
    if (!response.ok) {
      if (passwordRef.current) {passwordRef.current.value = "";}
      if (response.status === 429) {
        const at = Date.now() + Number(response.headers.get("Retry-After") ?? "900") * 1000; setRetryAt(at); setRateLimited(true);
        setState({ busy: false, error: `尝试次数过多，请在 ${new Intl.DateTimeFormat("zh-CN", { timeStyle: "medium" }).format(at)} 后重试` }); return;
      }
      const issue = await readAPIError(response, "用户名或密码不正确");
      setState({ busy: false, error: [401, 403].includes(response.status) ? "用户名或密码不正确" : issue.message, requestId: issue.requestId }); return;
    }
    acceptContext(await response.json() as AuthContext);
    const returnTo = safeReturnTo(new URLSearchParams(window.location.search).get("returnTo"));
    router.replace(returnTo); router.refresh();
  }
  return <AuthPanel eyebrow="欢迎回来" title="登录">
    {context.mode === "test" && context.testDefaultAccountActive ? <FeedbackBanner tone="info"><strong>测试模式已启用，默认管理员为 test / test。</strong><br />请勿将此服务器暴露到不受信网络。</FeedbackBanner> : null}
    <form className="auth-form" onSubmit={(event) => void submit(event)} aria-busy={state.busy}>
      <ErrorSummary message={state.error} requestId={state.requestId} errorRef={errorRef} />
      <div className="form-field"><label htmlFor="username">用户名</label><input autoComplete="username" id="username" name="username" required /></div>
      <div className="form-field"><label htmlFor="password">密码</label><input autoComplete="current-password" id="password" name="password" ref={passwordRef} required type="password" /></div>
      <button className="button auth-submit" disabled={state.busy || rateLimited} type="submit">{state.busy ? "正在登录…" : "登录"}</button>
    </form>
    <p className="auth-footnote">新账号需要管理员邀请。本阶段不提供自助找回密码。</p>
  </AuthPanel>;
}

type LinkInspection = components["schemas"]["AccountLinkInspection"];

function useCapability(fragmentName: "invite" | "reset", expectedKind: LinkInspection["kind"]) {
  const [result, setResult] = useState<{ state: "loading" | "invalid" | "ready"; token?: string; inspection?: LinkInspection }>({ state: "loading" });
  const started = useRef(false);
  useEffect(() => {
    if (started.current) {return;} started.current = true;
    const params = new URLSearchParams(window.location.hash.slice(1));
    const token = params.get(fragmentName);
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${window.location.search}`);
    if (!token) { window.setTimeout(() => setResult({ state: "invalid" }), 0); return; }
    void fetch("/api/v1/auth/account-links/inspect", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ expectedKind, token }) })
      .then(async (response) => {
        if (!response.ok) { setResult({ state: "invalid" }); return; }
        setResult({ state: "ready", token, inspection: await response.json() as LinkInspection });
      }).catch(() => setResult({ state: "invalid" }));
  }, [expectedKind, fragmentName]);
  return result;
}

function LinkUnavailable({ kind }: { kind: "invite" | "reset" }) {
  const noun = kind === "invite" ? "邀请" : "密码重置";
  return <AuthPanel eyebrow="链接不可用" title={`${noun}链接不可用`}><div className="link-unavailable" role="alert">此{noun}链接不可用。它可能已过期、已被使用或已被撤销，请联系管理员重新创建。</div></AuthPanel>;
}

export function RegisterForm() {
  const capability = useCapability("invite", "INVITATION");
  const { acceptContext } = useAuth(); const router = useRouter();
  const [state, setState] = useState(initialSubmit); const errorRef = useRef<HTMLDivElement>(null);
  if (capability.state === "loading") {return <AuthPanel eyebrow="账号邀请" title="正在检查邀请"><div className="auth-skeleton" role="status" aria-label="正在检查邀请" /></AuthPanel>;}
  if (capability.state !== "ready" || !capability.token || !capability.inspection) {return <LinkUnavailable kind="invite" />;}
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setState({ busy: true, error: null }); const values = new FormData(event.currentTarget);
    const response = await fetch("/api/v1/auth/invitations/accept", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ token: capability.token, username: values.get("username"), displayName: values.get("displayName"), password: values.get("password"), passwordConfirmation: values.get("passwordConfirmation") }) }).catch(() => null);
    if (!response) { setState({ busy: false, error: "无法连接服务器，请检查网络后重试" }); return; }
    if (!response.ok) { const issue = await readAPIError(response, "注册失败，请重试"); if (response.status === 404) { setState({ busy: false, error: "此邀请链接已不可用" }); return; } setState({ busy: false, error: issue.message, requestId: issue.requestId }); return; }
    acceptContext(await response.json() as AuthContext); router.replace("/"); router.refresh();
  }
  return <AuthPanel eyebrow="账号邀请" title="创建 Retrom 账号" width="wide">
    <div className="link-facts"><span>账号角色</span><strong>{capability.inspection.role === "ADMIN" ? "管理员" : "普通用户"}</strong><span>到期时间</span><strong>{formatAbsolute(capability.inspection.expiresAtMs)}</strong></div>
    {capability.inspection.role === "ADMIN" ? <FeedbackBanner tone="info">管理员可管理服务器内容和其他账号</FeedbackBanner> : null}
    <form className="auth-form" onSubmit={(event) => void submit(event)} aria-busy={state.busy}>
      <ErrorSummary message={state.error} requestId={state.requestId} errorRef={errorRef} />
      <div className="form-field"><label htmlFor="username">用户名</label><input autoComplete="username" id="username" name="username" required /></div>
      <div className="form-field"><label htmlFor="displayName">显示名称</label><input autoComplete="name" id="displayName" name="displayName" required /></div>
      <PasswordField autoComplete="new-password" label="密码" name="password" /><PasswordField autoComplete="new-password" label="确认密码" name="passwordConfirmation" />
      <PasswordPolicy />
      <button className="button auth-submit" disabled={state.busy} type="submit">{state.busy ? "正在创建账号…" : "创建账号并进入 Retrom"}</button>
    </form>
  </AuthPanel>;
}

export function ResetPasswordForm() {
  const capability = useCapability("reset", "PASSWORD_RESET");
  const { acceptContext } = useAuth(); const router = useRouter();
  const [state, setState] = useState(initialSubmit); const [disabledComplete, setDisabledComplete] = useState(false); const errorRef = useRef<HTMLDivElement>(null);
  if (capability.state === "loading") {return <AuthPanel eyebrow="账号恢复" title="正在检查链接"><div className="auth-skeleton" role="status" aria-label="正在检查链接" /></AuthPanel>;}
  if (capability.state !== "ready" || !capability.token || !capability.inspection) {return <LinkUnavailable kind="reset" />;}
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setState({ busy: true, error: null }); const values = new FormData(event.currentTarget);
    const response = await fetch("/api/v1/auth/password-resets/complete", { method: "POST", credentials: "same-origin", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ token: capability.token, password: values.get("password"), passwordConfirmation: values.get("passwordConfirmation") }) }).catch(() => null);
    if (!response) { setState({ busy: false, error: "无法连接服务器，请检查网络后重试" }); return; }
    if (!response.ok) { const issue = await readAPIError(response, "密码更新失败，请重试"); if (response.status === 404) { setState({ busy: false, error: "此密码重置链接已不可用" }); return; } setState({ busy: false, error: issue.message, requestId: issue.requestId }); return; }
    const payload = await response.json() as AuthContext | { status: string };
    if ("authenticationState" in payload) { acceptContext(payload); router.replace("/"); router.refresh(); return; }
    setDisabledComplete(true); setState(initialSubmit);
  }
  if (disabledComplete) {return <AuthPanel eyebrow="密码已更新" title="账号仍处于停用状态"><FeedbackBanner tone="info">密码已更新，但账号仍处于停用状态，请联系管理员</FeedbackBanner></AuthPanel>;}
  return <AuthPanel eyebrow="账号恢复" title="设置新密码">
    <div className="link-facts"><span>账号</span><strong>@{capability.inspection.username}</strong><span>到期时间</span><strong>{formatAbsolute(capability.inspection.expiresAtMs)}</strong></div>
    <form className="auth-form" onSubmit={(event) => void submit(event)} aria-busy={state.busy}><ErrorSummary message={state.error} requestId={state.requestId} errorRef={errorRef} />
      <PasswordField autoComplete="new-password" label="新密码" name="password" /><PasswordField autoComplete="new-password" label="确认密码" name="passwordConfirmation" />
      <PasswordPolicy />
      <button className="button auth-submit" disabled={state.busy} type="submit">{state.busy ? "正在更新密码…" : "更新密码"}</button>
    </form>
  </AuthPanel>;
}

export function AccountSettings() {
  const { context, acceptContext, authenticatedFetch } = useAuth();
  const [state, setState] = useState(initialSubmit); const [success, setSuccess] = useState(false); const errorRef = useRef<HTMLDivElement>(null);
  const user = context.user;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setState({ busy: true, error: null }); setSuccess(false); const form = event.currentTarget; const values = new FormData(form);
    const response = await authenticatedFetch("/api/v1/auth/change-password", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ currentPassword: values.get("currentPassword"), newPassword: values.get("newPassword"), newPasswordConfirmation: values.get("newPasswordConfirmation") }) }).catch(() => null);
    if (!response) { setState({ busy: false, error: "无法连接服务器，请检查网络后重试" }); return; }
    if (!response.ok) { const issue = await readAPIError(response, "密码更新失败，请重试"); setState({ busy: false, error: issue.message, requestId: issue.requestId }); return; }
    acceptContext(await response.json() as AuthContext); form.reset(); setState(initialSubmit); setSuccess(true);
  }
  return <div className="page-layout page-layout-detail"><header className="page-header"><div><p className="eyebrow">账号安全</p><h1 tabIndex={-1}>账户设置</h1><p>查看当前账号资料并更新登录密码。</p></div></header>
    <div className="account-settings-grid"><section className="panel"><div className="panel-head"><div><h2>账号资料</h2><p>本版本不提供自行修改用户名或显示名称。</p></div></div><dl className="account-facts"><div><dt>用户名</dt><dd>@{user?.username}</dd></div><div><dt>显示名称</dt><dd>{user?.displayName}</dd></div><div><dt>角色</dt><dd><StatusBadge tone={user?.role === "ADMIN" ? "info" : "neutral"}>{user?.role === "ADMIN" ? "管理员" : "普通用户"}</StatusBadge></dd></div><div><dt>账号状态</dt><dd><StatusBadge tone="good">启用</StatusBadge></dd></div></dl></section>
      <section className="panel"><div className="panel-head"><div><h2>修改密码</h2><p>更新后，其他设备上的会话将立即退出。</p></div></div><div className="panel-body">{success ? <FeedbackBanner tone="good">密码已更新，其他设备已退出登录</FeedbackBanner> : null}<form className="auth-form account-password-form" onSubmit={(event) => void submit(event)} aria-busy={state.busy}><ErrorSummary message={state.error} requestId={state.requestId} errorRef={errorRef} /><PasswordField autoComplete="current-password" label="当前密码" name="currentPassword" /><PasswordField autoComplete="new-password" label="新密码" name="newPassword" /><PasswordField autoComplete="new-password" label="确认密码" name="newPasswordConfirmation" /><PasswordPolicy /><button className="button" disabled={state.busy} type="submit">{state.busy ? "正在更新…" : "更新密码"}</button></form></div></section></div>
  </div>;
}

function formatAbsolute(value: number) {
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "short" }).format(value);
}
