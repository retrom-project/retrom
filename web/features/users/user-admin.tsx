"use client";

import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { EmptyState, FeedbackBanner, StatusBadge } from "@/components/ui";
import { useAuth } from "@/features/auth/auth-provider";
import { readAPIError, type AccountRole, type AccountStatus } from "@/features/auth/types";
import { newUuid } from "@/lib/crypto";
import type { components } from "@/lib/api/generated/schema";

export type AdminUser = components["schemas"]["AdminUser"];
export type AccountLink = components["schemas"]["AccountLink"];
export type UserPage = components["schemas"]["AdminUserList"];
export type LinkPage = components["schemas"]["AccountLinkList"];

type OneTimeResult = { kind: "INVITATION" | "PASSWORD_RESET"; url: string; role?: AccountRole; expiresAtMs: number };
type ManageState = {
  user: AdminUser; etag: string; lastEnabledAdmin: boolean; role: AccountRole; status: "ENABLED" | "DISABLED";
  error: string; requestId?: string; busy: boolean; key: string;
};

const roleLabels: Record<AccountRole, string> = { ADMIN: "管理员", USER: "普通用户" };
const statusLabels: Record<AccountStatus, string> = { ENABLED: "启用", DISABLED: "停用", DELETED: "已删除" };

function absoluteTime(value: number | null) {
  if (!value) return "从未登录";
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "short" }).format(value);
}

function responseETag(response: Response, version: number) {
  return response.headers.get("ETag") ?? `"v${version}"`;
}

function Drawer({ open, title, description, children, onClose }: { open: boolean; title: string; description: string; children: ReactNode; onClose: () => void }) {
  const panel = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!open) return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    panel.current?.querySelector<HTMLElement>("button:not(:disabled), input:not(:disabled), select:not(:disabled)")?.focus();
    return () => previous?.focus();
  }, [open]);
  if (!open) return null;
  return <div className="drawer-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
    <section className="app-drawer" ref={panel} role="dialog" aria-modal="true" aria-labelledby="drawer-title" onKeyDown={(event) => {
      if (event.key === "Escape") onClose();
      if (event.key !== "Tab") return;
      const focusable = Array.from(panel.current?.querySelectorAll<HTMLElement>("button:not(:disabled), input:not(:disabled), select:not(:disabled), [href], [tabindex]:not([tabindex='-1'])") ?? []);
      if (!focusable.length) return; const first = focusable[0]; const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    }}>
      <header><div><h2 id="drawer-title">{title}</h2><p>{description}</p></div><button aria-label="关闭" type="button" onClick={onClose}>×</button></header>
      <div className="drawer-body">{children}</div>
    </section>
  </div>;
}

export function UserAdmin({ initialUsers, initialInvitations, filterValues }: { initialUsers: UserPage; initialInvitations: LinkPage; filterValues: Record<string, string> }) {
  const { context, authenticatedFetch } = useAuth();
  const router = useRouter();
  const titleRef = useRef<HTMLHeadingElement>(null);
  const [users, setUsers] = useState(initialUsers);
  const [invitations, setInvitations] = useState(initialInvitations);
  const [invitationState, setInvitationState] = useState("ACTIVE");
  const [invitationDrawer, setInvitationDrawer] = useState(false);
  const [inviteRole, setInviteRole] = useState<AccountRole>("USER");
  const [inviteConfirmed, setInviteConfirmed] = useState(false);
  const [inviteBusy, setInviteBusy] = useState(false);
  const [inviteError, setInviteError] = useState("");
  const [inviteKey, setInviteKey] = useState(newUuid);
  const [oneTime, setOneTime] = useState<OneTimeResult | null>(null);
  const [manage, setManage] = useState<ManageState | null>(null);
  const [manageConfirmation, setManageConfirmation] = useState<"PROMOTE" | "DISABLE" | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteConfirmation, setDeleteConfirmation] = useState("");
  const [deleteKey, setDeleteKey] = useState(newUuid);
  const [revoke, setRevoke] = useState<AccountLink | null>(null);
  const [revokeBusy, setRevokeBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [online, setOnline] = useState(true);
  const resetKey = useRef(newUuid());

  useEffect(() => { titleRef.current?.focus(); }, []);
  useEffect(() => {
    const update = () => setOnline(navigator.onLine);
    window.addEventListener("online", update); window.addEventListener("offline", update); update();
    return () => { window.removeEventListener("online", update); window.removeEventListener("offline", update); };
  }, []);

  const currentUserID = context.user?.userId;
  const noUsers = users.items.length === 0;

  function applyFilters(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); const query = new URLSearchParams();
    for (const name of ["q", "role", "status", "sort"]) { const value = String(data.get(name) ?? "").trim(); if (value) query.set(name, value); }
    router.push(`/admin/users${query.size ? `?${query}` : ""}`);
  }

  async function loadMoreUsers() {
    if (!users.nextCursor) return;
    const query = new URLSearchParams(filterValues); query.set("cursor", users.nextCursor); query.set("limit", "50");
    const response = await authenticatedFetch(`/api/v1/admin/users?${query}`, { cache: "no-store" });
    if (!response.ok) { setNotice("无法加载更多用户，请重试"); return; }
    const page = await response.json() as UserPage;
    setUsers((current) => ({ ...page, items: [...current.items, ...page.items] }));
  }

  async function loadInvitations(state = invitationState) {
    const response = await authenticatedFetch(`/api/v1/admin/invitations?state=${state}&limit=50`, { cache: "no-store" });
    if (!response.ok) { setNotice("无法刷新邀请列表，请重试"); return; }
    setInvitations(await response.json() as LinkPage);
  }

  async function openUser(userId: string, concurrencyMessage = "") {
    setNotice("");
    const [detailResponse, adminsResponse] = await Promise.all([
      authenticatedFetch(`/api/v1/admin/users/${userId}`, { cache: "no-store" }),
      authenticatedFetch("/api/v1/admin/users?role=ADMIN&status=ENABLED&limit=2", { cache: "no-store" })
    ]);
    if (!detailResponse.ok || !adminsResponse.ok) { setNotice("无法读取用户最新状态，请重试"); return; }
    const user = await detailResponse.json() as AdminUser; const admins = await adminsResponse.json() as UserPage;
    setManage({ user, etag: responseETag(detailResponse, user.version), lastEnabledAdmin: user.role === "ADMIN" && user.status === "ENABLED" && admins.items.length === 1 && admins.items[0]?.userId === user.userId, role: user.role, status: user.status === "DISABLED" ? "DISABLED" : "ENABLED", error: concurrencyMessage, busy: false, key: newUuid() });
  }

  function changeManaged(values: Partial<Pick<ManageState, "role" | "status">>) {
    setManage((current) => current ? { ...current, ...values, key: newUuid(), error: "" } : current);
  }

  function requestManagedSave() {
    if (!manage) return;
    if (manage.user.role !== "ADMIN" && manage.role === "ADMIN") { setManageConfirmation("PROMOTE"); return; }
    if (manage.user.status === "ENABLED" && manage.status === "DISABLED") { setManageConfirmation("DISABLE"); return; }
    void patchManaged();
  }

  async function patchManaged() {
    if (!manage) return; setManageConfirmation(null); setManage((current) => current ? { ...current, busy: true, error: "" } : current);
    const body: { role?: AccountRole; status?: "ENABLED" | "DISABLED"; confirmAdminRole: boolean } = { confirmAdminRole: manage.user.role !== "ADMIN" && manage.role === "ADMIN" };
    if (manage.role !== manage.user.role) body.role = manage.role;
    if (manage.status !== manage.user.status) body.status = manage.status;
    const response = await authenticatedFetch(`/api/v1/admin/users/${manage.user.userId}`, { method: "PATCH", headers: { "Content-Type": "application/json", "If-Match": manage.etag, "Idempotency-Key": manage.key }, body: JSON.stringify(body) }).catch(() => null);
    if (!response) { setManage((current) => current ? { ...current, busy: false, error: "网络结果未知；请先检查账号最新状态，再决定是否重试" } : current); return; }
    if (response.status === 412) { await openUser(manage.user.userId, "账号已被其他管理员修改，请确认最新状态后重试"); return; }
    if (!response.ok) { const issue = await readAPIError(response, "无法更新账号"); setManage((current) => current ? { ...current, busy: false, error: issue.message, requestId: issue.requestId } : current); return; }
    const updated = await response.json() as AdminUser;
    setUsers((current) => ({ ...current, items: current.items.map((item) => item.userId === updated.userId ? updated : item) }));
    setManage((current) => current ? { ...current, user: updated, role: updated.role, status: updated.status === "DISABLED" ? "DISABLED" : "ENABLED", etag: responseETag(response, updated.version), busy: false, key: newUuid() } : current);
    setNotice("账号安全状态已更新");
  }

  async function deleteManaged() {
    if (!manage || deleteConfirmation !== manage.user.username) return;
    setManage((current) => current ? { ...current, busy: true } : current);
    const response = await authenticatedFetch(`/api/v1/admin/users/${manage.user.userId}`, { method: "DELETE", headers: { "Content-Type": "application/json", "If-Match": manage.etag, "Idempotency-Key": deleteKey }, body: JSON.stringify({ confirmUsername: deleteConfirmation }) }).catch(() => null);
    if (!response) { setManage((current) => current ? { ...current, busy: false, error: "网络结果未知；请检查用户列表后再重试" } : current); return; }
    if (response.status === 412) { setDeleteOpen(false); await openUser(manage.user.userId, "账号已被其他管理员修改，请确认最新状态后重试"); return; }
    if (!response.ok) { const issue = await readAPIError(response, "无法删除账号"); setManage((current) => current ? { ...current, busy: false, error: issue.message } : current); return; }
    setDeleteOpen(false); setManage(null); setUsers((current) => ({ ...current, items: current.items.filter((item) => item.userId !== manage.user.userId) })); setNotice("账号已删除"); router.refresh();
  }

  async function createResetLink() {
    if (!manage) return; setManage((current) => current ? { ...current, busy: true, error: "" } : current);
    const response = await authenticatedFetch(`/api/v1/admin/users/${manage.user.userId}/password-reset-links`, { method: "POST", headers: { "Content-Type": "application/json", "If-Match": manage.etag, "Idempotency-Key": resetKey.current }, body: "{}" }).catch(() => null);
    if (!response) { setManage((current) => current ? { ...current, busy: false, error: "网络结果未知；请检查该用户最新状态后再重试" } : current); return; }
    if (response.status === 412) { await openUser(manage.user.userId, "账号已被其他管理员修改，请确认最新状态后重试"); return; }
    if (!response.ok) { const issue = await readAPIError(response, "无法创建密码重置链接"); setManage((current) => current ? { ...current, busy: false, error: issue.message } : current); return; }
    const result = await response.json() as { url: string; expiresAtMs: number; targetUserVersion: number };
    setManage((current) => current ? { ...current, user: { ...current.user, version: result.targetUserVersion }, etag: responseETag(response, result.targetUserVersion), busy: false } : current);
    resetKey.current = newUuid(); setOneTime({ kind: "PASSWORD_RESET", url: result.url, expiresAtMs: result.expiresAtMs });
  }

  async function createInvitation(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (inviteRole === "ADMIN" && !inviteConfirmed) return;
    setInviteBusy(true); setInviteError("");
    const response = await authenticatedFetch("/api/v1/admin/invitations", { method: "POST", headers: { "Content-Type": "application/json", "Idempotency-Key": inviteKey }, body: JSON.stringify({ role: inviteRole, confirmAdminRole: inviteRole === "ADMIN" && inviteConfirmed }) }).catch(() => null);
    if (!response) { setInviteBusy(false); setInviteError("网络结果未知；请检查邀请列表后再决定是否重试"); return; }
    if (!response.ok) { const issue = await readAPIError(response, "无法创建邀请"); setInviteBusy(false); setInviteError(issue.message); return; }
    const result = await response.json() as { url: string; role: AccountRole; expiresAtMs: number };
    setInviteBusy(false); setInvitationDrawer(false); setOneTime({ kind: "INVITATION", url: result.url, role: result.role, expiresAtMs: result.expiresAtMs });
  }

  function closeOneTime() {
    const invitation = oneTime?.kind === "INVITATION"; setOneTime(null);
    if (invitation) { setInviteRole("USER"); setInviteConfirmed(false); setInviteKey(newUuid()); void loadInvitations(); }
  }

  async function revokeInvitation() {
    if (!revoke) return; setRevokeBusy(true);
    const response = await authenticatedFetch(`/api/v1/admin/account-links/${revoke.accountLinkId}`, { method: "DELETE", headers: { "Content-Type": "application/json", "If-Match": `"v${revoke.version}"`, "Idempotency-Key": newUuid() }, body: "{}" }).catch(() => null);
    setRevokeBusy(false);
    if (!response?.ok) { setNotice(response ? "邀请状态已变化，请刷新列表" : "网络结果未知，请刷新邀请列表确认状态"); setRevoke(null); await loadInvitations(); return; }
    setRevoke(null); setNotice("邀请已撤销"); await loadInvitations();
  }

  const managedIsSelf = manage?.user.userId === currentUserID;
  const managedLocked = Boolean(managedIsSelf || manage?.lastEnabledAdmin || manage?.user.status === "DELETED");
  const managedChanged = Boolean(manage && (manage.role !== manage.user.role || manage.status !== manage.user.status));

  return <div className="page-layout page-layout-admin user-admin-page">
    <header className="page-header"><div><p className="eyebrow">账号与安全</p><h1 ref={titleRef} tabIndex={-1}>用户管理</h1><p>创建邀请并管理谁可以登录；游玩记录和存档始终保持私有。</p></div><div className="header-actions"><button className="button" type="button" disabled={!online} onClick={() => setInvitationDrawer(true)}>创建邀请</button></div></header>
    {!online ? <FeedbackBanner tone="bad">当前处于离线状态；已显示的非秘密信息会保留，写操作已暂停。</FeedbackBanner> : null}
    {notice ? <FeedbackBanner tone="info">{notice}</FeedbackBanner> : null}
    <form className="filter-bar user-filters" onSubmit={applyFilters}><div className="filter-controls">
      <label className="filter-control filter-search"><span className="filter-label">搜索</span><input className="select filter-text" name="q" defaultValue={filterValues.q ?? ""} placeholder="用户名或显示名称" /></label>
      <label className="filter-control"><span className="filter-label">角色</span><select className="select" name="role" defaultValue={filterValues.role ?? ""}><option value="">全部</option><option value="ADMIN">管理员</option><option value="USER">普通用户</option></select></label>
      <label className="filter-control"><span className="filter-label">状态</span><select className="select" name="status" defaultValue={filterValues.status ?? ""}><option value="">启用与停用</option><option value="ENABLED">启用</option><option value="DISABLED">停用</option><option value="DELETED">已删除</option><option value="ALL">全部</option></select></label>
      <label className="filter-control"><span className="filter-label">排序</span><select className="select" name="sort" defaultValue={filterValues.sort ?? ""}><option value="">最近创建</option><option value="USERNAME_ASC">用户名 A–Z</option><option value="LAST_LOGIN_DESC">最近登录</option></select></label>
      <button className="button filter-submit" type="submit">应用筛选</button>
    </div><div className="filter-summary"><span className="filter-hint">已加载 <strong>{users.items.length}</strong> 个账号</span><button className="button secondary filter-reset" type="button" onClick={() => router.push("/admin/users")}>重置</button></div></form>
    {noUsers ? <EmptyState title="无法读取用户列表" description="实例必须至少有一名启用管理员。请重试；若问题持续存在，请检查服务器状态。" /> : <section className="panel user-table-panel"><div className="table-wrap user-table-wrap"><table className="user-table"><thead><tr><th>用户</th><th>角色</th><th>状态</th><th>最近登录</th><th>创建时间</th><th>活跃会话</th><th>操作</th></tr></thead><tbody>{users.items.map((user) => <tr key={user.userId}><td><strong>{user.status === "DELETED" ? "已删除用户" : user.displayName}</strong><small>@{user.username}</small></td><td>{roleLabels[user.role]}</td><td><StatusBadge tone={user.status === "ENABLED" ? "good" : user.status === "DISABLED" ? "warn" : "bad"}>{statusLabels[user.status]}</StatusBadge></td><td>{absoluteTime(user.lastLoginAtMs)}</td><td>{absoluteTime(user.createdAtMs)}</td><td>{user.activeSessionCount}</td><td><button className="button secondary table-action" type="button" onClick={() => void openUser(user.userId)}>管理</button></td></tr>)}</tbody></table></div>{users.nextCursor ? <div className="load-more-row"><button className="button secondary" type="button" onClick={() => void loadMoreUsers()}>加载更多用户</button></div> : null}</section>}
    <InvitationList page={invitations} state={invitationState} onState={(value) => { setInvitationState(value); void loadInvitations(value); }} onRevoke={setRevoke} />

    <Drawer open={invitationDrawer} title="创建邀请" description="完整链接只会在创建成功后显示一次。" onClose={() => { if (!inviteBusy) setInvitationDrawer(false); }}><form className="drawer-form" onSubmit={(event) => void createInvitation(event)}><label className="form-field"><span>账号角色</span><select value={inviteRole} onChange={(event) => { setInviteRole(event.target.value as AccountRole); setInviteConfirmed(false); setInviteKey(newUuid()); }}><option value="USER">普通用户</option><option value="ADMIN">管理员</option></select></label>{inviteRole === "ADMIN" ? <div className="admin-role-warning"><strong>管理员可管理共享游戏内容、服务器设置和账号。</strong><label><input type="checkbox" checked={inviteConfirmed} onChange={(event) => { setInviteConfirmed(event.target.checked); setInviteKey(newUuid()); }} />我确认此邀请将创建管理员账号</label></div> : null}{inviteError ? <div className="form-error" role="alert">{inviteError}</div> : null}<div className="drawer-actions"><button className="button secondary" type="button" disabled={inviteBusy} onClick={() => setInvitationDrawer(false)}>取消</button><button className="button" type="submit" disabled={!online || inviteBusy || inviteRole === "ADMIN" && !inviteConfirmed}>{inviteBusy ? "正在创建…" : "创建邀请"}</button></div></form></Drawer>

    <Drawer open={Boolean(manage)} title="管理用户" description="只管理账号与安全状态，不提供他人的私有游戏数据。" onClose={() => { if (!manage?.busy) setManage(null); }}>{manage ? <div className="drawer-form"><div className="managed-identity"><span>{manage.user.displayName.slice(0, 1).toUpperCase()}</span><div><strong>{manage.user.displayName}</strong><small>@{manage.user.username}</small></div></div>{managedIsSelf ? <FeedbackBanner tone="info">不能修改当前登录账号</FeedbackBanner> : null}{manage.lastEnabledAdmin ? <FeedbackBanner tone="info">服务器必须保留至少一名启用管理员</FeedbackBanner> : null}{manage.error ? <div className="form-error" role="alert"><strong>{manage.error}</strong>{manage.requestId ? <small>请求 ID：{manage.requestId}</small> : null}</div> : null}<label className="form-field"><span>角色</span><select value={manage.role} disabled={managedLocked || manage.busy} onChange={(event) => changeManaged({ role: event.target.value as AccountRole })}><option value="USER">普通用户</option><option value="ADMIN">管理员</option></select></label><label className="form-field"><span>状态</span><select value={manage.status} disabled={managedLocked || manage.busy} onChange={(event) => changeManaged({ status: event.target.value as "ENABLED" | "DISABLED" })}><option value="ENABLED">启用</option><option value="DISABLED">停用</option></select></label><div className="drawer-actions"><button className="button secondary" type="button" disabled={manage.busy} onClick={() => void createResetLink()}>创建密码重置链接</button><button className="button" type="button" disabled={!online || managedLocked || manage.busy || !managedChanged} onClick={requestManagedSave}>{manage.busy ? "正在保存…" : "保存更改"}</button></div><div className="danger-zone"><h3>删除账号</h3><p>删除不可恢复；私有数据会保留，但管理员不能查看。</p><button className="button danger" type="button" disabled={!online || managedLocked || manage.busy} onClick={() => { setDeleteConfirmation(""); setDeleteKey(newUuid()); setDeleteOpen(true); }}>删除账号</button></div></div> : null}</Drawer>

    <ConfirmDialog open={manageConfirmation === "PROMOTE"} title="确认授予管理员权限" description="升级后，该账号将获得服务器管理能力。" confirmLabel="确认升级" onCancel={() => setManageConfirmation(null)} onConfirm={() => void patchManaged()}><ul><li>可管理共享游戏内容</li><li>可管理服务器设置</li><li>可管理其他账号</li></ul></ConfirmDialog>
    <ConfirmDialog open={manageConfirmation === "DISABLE"} title="确认停用账号" description="停用会立即收回该账号的访问能力。" confirmLabel="停用账号" tone="danger" onCancel={() => setManageConfirmation(null)} onConfirm={() => void patchManaged()}><ul><li>立即退出所有设备</li><li>终止待用启动会话</li><li>撤销待用注册链接与重置链接</li><li>保留私有数据且不向管理员开放</li></ul></ConfirmDialog>
    <ConfirmDialog open={deleteOpen} title="永久删除账号" description={manage ? `输入完整用户名 ${manage.user.username} 以确认。` : ""} confirmLabel="删除账号" tone="danger" busy={Boolean(manage?.busy)} confirmDisabled={!manage || deleteConfirmation !== manage.user.username} onCancel={() => setDeleteOpen(false)} onConfirm={() => void deleteManaged()}><ul><li>账号不可登录且不可恢复</li><li>数据保留但不向管理员开放</li><li>用户名不可复用</li></ul><label className="delete-confirm-field">确认用户名<input value={deleteConfirmation} onChange={(event) => setDeleteConfirmation(event.target.value)} /></label></ConfirmDialog>
    <ConfirmDialog open={Boolean(revoke)} title="撤销邀请" description="撤销后，持有该链接的人将无法再用它注册。" confirmLabel="撤销邀请" tone="danger" busy={revokeBusy} onCancel={() => setRevoke(null)} onConfirm={() => void revokeInvitation()} />
    <OneTimeLinkDialog key={oneTime?.url ?? "closed"} result={oneTime} onClose={closeOneTime} />
  </div>;
}

function InvitationList({ page, state, onState, onRevoke }: { page: LinkPage; state: string; onState: (state: string) => void; onRevoke: (link: AccountLink) => void }) {
  return <details className="panel invitation-list" open><summary><span>待用邀请</span><small>列表永不显示完整链接或 token</small></summary><div className="invitation-toolbar"><label>状态<select value={state} onChange={(event) => onState(event.target.value)}><option value="ACTIVE">待使用</option><option value="CONSUMED">已使用</option><option value="REVOKED">已撤销</option><option value="EXPIRED">已过期</option><option value="ALL">全部</option></select></label></div>{page.items.length ? <div className="table-wrap"><table><thead><tr><th>角色</th><th>创建者</th><th>创建时间</th><th>到期时间</th><th>状态</th><th>操作</th></tr></thead><tbody>{page.items.map((link) => <tr key={link.accountLinkId}><td>{link.role ? roleLabels[link.role] : "—"}</td><td>{link.createdBy ? `@${link.createdBy.username}` : "系统"}</td><td>{absoluteTime(link.createdAtMs)}</td><td>{absoluteTime(link.expiresAtMs)}</td><td><StatusBadge tone={link.state === "ACTIVE" ? "good" : "neutral"}>{link.state === "ACTIVE" ? "待使用" : link.state === "CONSUMED" ? "已使用" : link.state === "REVOKED" ? "已撤销" : "已过期"}</StatusBadge></td><td>{link.state === "ACTIVE" ? <button className="button secondary table-action" type="button" onClick={() => onRevoke(link)}>撤销</button> : "—"}</td></tr>)}</tbody></table></div> : <div className="compact-empty">当前筛选下没有邀请。</div>}</details>;
}

function OneTimeLinkDialog({ result, onClose }: { result: OneTimeResult | null; onClose: () => void }) {
  const input = useRef<HTMLInputElement>(null); const [copied, setCopied] = useState(false); const [fallback, setFallback] = useState(false);
  async function copy() {
    if (!result) return;
    try { await navigator.clipboard.writeText(result.url); setCopied(true); setFallback(false); window.setTimeout(() => setCopied(false), 1600); }
    catch { input.current?.focus(); input.current?.select(); setFallback(true); }
  }
  if (!result) return null;
  const invitation = result.kind === "INVITATION";
  return <ConfirmDialog open title={invitation ? "邀请已创建" : "密码重置链接已创建"} description={invitation ? "一小时后或注册完成后失效" : "一小时后或密码更新后失效"} confirmLabel="完成" hideCancel onCancel={onClose} onConfirm={onClose}><div className="one-time-dialog"><div className="one-time-body"><label>一次性链接<input ref={input} readOnly value={result.url} onFocus={(event) => event.currentTarget.select()} /></label><button className="button secondary" type="button" onClick={() => void copy()}>{copied ? "已复制" : "复制链接"}</button></div><dl><div><dt>{invitation ? "角色" : "用途"}</dt><dd>{invitation && result.role ? roleLabels[result.role] : "密码重置"}</dd></div><div><dt>到期时间</dt><dd>{absoluteTime(result.expiresAtMs)}</dd></div></dl>{fallback ? <p className="copy-fallback" role="status">无法自动复制，请按 Ctrl+C</p> : null}<p className="one-time-warning">关闭后无法再次查看完整链接</p></div></ConfirmDialog>;
}
