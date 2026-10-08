"use client";
import { useState } from "react";
import type { Schema } from "@/lib/api/types";
import { useAuth } from "@/features/auth/auth-provider";
import { FeedbackBanner } from "@/components/ui";
export function UserSearch({
  onSearch,
}: {
  onSearch: (value: string) => void;
}) {
  const [search, setSearch] = useState("");
  return (
    <form
      className="panel user-filters"
      onSubmit={(event) => {
        event.preventDefault();
        onSearch(search);
      }}
    >
      <label className="field">
        <span className="field-label">搜索</span>
        <input
          value={search}
          placeholder="用户名或显示名称"
          onChange={(event) => setSearch(event.target.value)}
        />
      </label>
      <button className="button" type="submit">
        应用筛选
      </button>
    </form>
  );
}
export function InvitationForm({
  busy,
  error,
  onCancel,
  onSubmit,
}: {
  busy: boolean;
  error: string;
  onCancel: () => void;
  onSubmit: (role: "admin" | "user", hours: number) => void;
}) {
  const [role, setRole] = useState<"admin" | "user">("user");
  const [hours, setHours] = useState(72);
  return (
    <form
      className="drawer-form"
      onSubmit={(event) => {
        event.preventDefault();
        onSubmit(role, hours);
      }}
    >
      <label className="form-field">
        <span>账号角色</span>
        <select
          aria-label="账号角色"
          value={role}
          onChange={(event) =>
            setRole(event.target.value === "admin" ? "admin" : "user")
          }
        >
          <option value="user">普通用户</option>
          <option value="admin">管理员</option>
        </select>
      </label>
      <label className="form-field">
        <span>邀请有效期</span>
        <select
          aria-label="邀请有效期"
          value={hours}
          onChange={(event) => setHours(Number(event.target.value))}
        >
          <option value={24}>1 天</option>
          <option value={72}>3 天</option>
          <option value={168}>7 天</option>
        </select>
      </label>
      {role === "admin" ? (
        <p className="admin-role-warning">
          管理员可以管理共享游戏、目录和其他账号。请仅邀请可信任的人。
        </p>
      ) : null}
      {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
      <div className="drawer-actions">
        <button
          className="button secondary"
          type="button"
          disabled={busy}
          onClick={onCancel}
        >
          取消
        </button>
        <button className="button" disabled={busy}>
          {busy ? "正在创建…" : "创建邀请"}
        </button>
      </div>
    </form>
  );
}
export function UserEditor({
  user,
  busy,
  error,
  onChange,
  onSave,
  onReset,
  onDelete,
}: {
  user: Schema<"User">;
  busy: boolean;
  error: string;
  onChange: (user: Schema<"User">) => void;
  onSave: () => void;
  onReset: () => void;
  onDelete: () => void;
}) {
  const { context } = useAuth();
  const isSelf = context?.user?.id === user.id;
  const unavailable = isSelf || user.status === "deleted";
  return (
    <form
      className="drawer-form"
      onSubmit={(event) => {
        event.preventDefault();
        onSave();
      }}
    >
      <div className="managed-identity">
        <span>{user.displayName.slice(0, 1).toUpperCase()}</span>
        <div>
          <strong>{user.displayName}</strong>
          <small>@{user.username}</small>
        </div>
      </div>
      {unavailable ? (
        <FeedbackBanner tone="info">
          {isSelf ? "不能修改当前登录账号" : "这个账号已删除"}
        </FeedbackBanner>
      ) : null}
      <label className="form-field">
        <span>显示名称</span>
        <input
          value={user.displayName}
          disabled={busy || unavailable}
          required
          onChange={(event) =>
            onChange({ ...user, displayName: event.target.value })
          }
        />
      </label>
      <label className="form-field">
        <span>角色</span>
        <select
          aria-label="角色"
          value={user.role}
          disabled={busy || unavailable}
          onChange={(event) =>
            onChange({
              ...user,
              role: event.target.value === "admin" ? "admin" : "user",
            })
          }
        >
          <option value="user">普通用户</option>
          <option value="admin">管理员</option>
        </select>
      </label>
      <label className="form-field">
        <span>状态</span>
        <select
          aria-label="状态"
          value={user.status}
          disabled={busy || unavailable}
          onChange={(event) =>
            onChange({
              ...user,
              status: event.target.value === "active" ? "active" : "disabled",
            })
          }
        >
          <option value="active">启用</option>
          <option value="disabled">停用</option>
          {user.status === "deleted" ? (
            <option value="deleted">已删除</option>
          ) : null}
        </select>
      </label>
      {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
      <div className="drawer-actions">
        <button
          className="button secondary"
          type="button"
          disabled={busy || user.status === "deleted"}
          onClick={onReset}
        >
          创建密码重置链接
        </button>
        <button className="button" disabled={busy || unavailable}>
          {busy ? "正在保存…" : "保存更改"}
        </button>
      </div>
      <section className="danger-zone">
        <h3>删除账号</h3>
        <p>账号删除后无法登录，其私有游戏数据随后清理。</p>
        <button
          className="button danger"
          type="button"
          disabled={busy || unavailable}
          onClick={onDelete}
        >
          删除账号
        </button>
      </section>
    </form>
  );
}
