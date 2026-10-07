"use client";
import { AccountLinkList } from "./account-link-list";
import { UserTable } from "./user-table";
import { useCallback, useState } from "react";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { PageHeader } from "@/components/ui";
import { ResourceState } from "@/components/resource-state";
import { ConfirmDialog } from "@/components/confirm-dialog";
export function UserManager() {
  const [q, setQ] = useState("");
  const [offset, setOffset] = useState(0);
  const loader = useCallback(
    async () =>
      result(
        await api.GET("/api/v1/admin/users", {
          params: { query: { offset, limit: 24, q } },
        }),
      ),
    [offset, q],
  );
  const users = useResource(loader);
  const [deleting,setDeleting]=useState<Schema<"User">|null>(null);
  const [link, setLink] = useState<Schema<"AccountLink"> | null>(null);
  const [editing, setEditing] = useState<Schema<"User"> | null>(null);
  const [error, setError] = useState("");
  async function invitation(role: "admin" | "user") {
    try {
      setLink(
        result(
          await api.POST("/api/v1/admin/invitations", {
            body: { role, expiresInHours: 72 },
          }),
        ),
      );
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "邀请创建失败。");
    }
  }
  async function reset(userId: string) {
    try {
      setLink(
        result(
          await api.POST("/api/v1/admin/users/{userId}/password-reset-links", {
            params: { path: { userId } },
            body: { expiresInHours: 72 },
          }),
        ),
      );
    } catch (failure) {
      setError(
        failure instanceof Error ? failure.message : "重置链接创建失败。",
      );
    }
  }
  async function save() {
    if (!editing) {
      return;
    }
    try {
      result(
        await api.PATCH("/api/v1/admin/users/{userId}", {
          params: { path: { userId: editing.id } },
          body: {
            version: editing.version,
            displayName: editing.displayName,
            role: editing.role,
            status: editing.status,
          },
        }),
      );
      setEditing(null);
      users.reload();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "账号更新失败。");
    }
  }
  async function removeUser() {
    if(!deleting){return;}
    const response=await api.PATCH("/api/v1/admin/users/{userId}",{params:{path:{userId:deleting.id}},body:{version:deleting.version,displayName:deleting.displayName,role:deleting.role,status:"deleted"}});
    if(response.error){setError(response.error.message);return;}
    setDeleting(null);setEditing(null);users.reload();
  }
  async function revoke() {
    if (!link) {
      return;
    }
    const response = await api.DELETE(
      "/api/v1/admin/account-links/{accountLinkId}",
      {
        params: { path: { accountLinkId: link.id } },
        body: { version: link.version },
      },
    );
    if (response.error) {
      setError(response.error.message);
      return;
    }
    setLink(null);
  }
  return (
    <>
      <PageHeader
        title="用户管理"
        description="管理账号与安全状态。收藏、存档和最近游玩保持用户私有。"
        actions={
          <div className="workspace-actions">
            <button className="button" onClick={() => void invitation("user")}>
              邀请用户
            </button>
            <button
              className="button secondary"
              onClick={() => void invitation("admin")}
            >
              邀请管理员
            </button>
          </div>
        }
      />
      {error ? <p role="alert">{error}</p> : null}
      <div className="panel workspace-section">
        <label className="field">
          搜索用户
          <input
            value={q}
            placeholder="用户名或显示名称"
            onChange={(event) => {
              setQ(event.target.value);
              setOffset(0);
            }}
          />
        </label>
      </div>
      <ResourceState resource={users}>
        {(data) => (
          <>
            <UserTable
              users={data.items}
              onEdit={setEditing}
              onReset={(id) => void reset(id)}
            />
            <div className="library-pagination">
              <button
                className="button secondary"
                disabled={offset === 0}
                onClick={() => setOffset(offset - 24)}
              >
                上一页
              </button>
              <span>{data.total} 位用户</span>
              <button
                className="button secondary"
                disabled={offset + data.items.length >= data.total}
                onClick={() => setOffset(offset + 24)}
              >
                下一页
              </button>
            </div>
          </>
        )}
      </ResourceState>
      <AccountLinkList key={link?.id} />
      <ConfirmDialog
        open={editing !== null}
        title="编辑账号"
        onCancel={() => setEditing(null)}
        onConfirm={() => void save()}
      >
        {editing ? (
          <div className="stack">
            <label className="field">
              显示名称
              <input
                value={editing.displayName}
                onChange={(event) =>
                  setEditing({ ...editing, displayName: event.target.value })
                }
              />
            </label>
            <label className="field">
              角色
              <select
                value={editing.role}
                onChange={(event) => {
                  if (
                    event.target.value === "admin" ||
                    event.target.value === "user"
                  ) {
                    setEditing({ ...editing, role: event.target.value });
                  }
                }}
              >
                <option value="user">普通用户</option>
                <option value="admin">管理员</option>
              </select>
            </label>
            <label className="field">
              账号状态
              <select
                value={editing.status}
                onChange={(event) => {
                  if (
                    event.target.value === "active" ||
                    event.target.value === "disabled"
                  ) {
                    setEditing({ ...editing, status: event.target.value });
                  }
                }}
              >
                <option value="active">正常</option>
                <option value="disabled">停用</option>
              </select>
            </label>
            <button className="button danger" onClick={()=>setDeleting(editing)}>删除账号</button>
            {error ? <p role="alert">{error}</p> : null}
          </div>
        ) : null}
      </ConfirmDialog>
      <ConfirmDialog open={!!deleting} title="删除账号" description="这个账号将无法登录，其私有游戏数据随后清理。" tone="danger" onCancel={()=>setDeleting(null)} onConfirm={()=>void removeUser()}>{error?<p role="alert">{error}</p>:null}</ConfirmDialog>
      <ConfirmDialog
        open={link !== null}
        title={link?.kind === "invitation" ? "账号邀请链接" : "密码重置链接"}
        confirmLabel="关闭"
        secondaryLabel="撤销链接"
        onCancel={() => setLink(null)}
        onConfirm={() => setLink(null)}
        onSecondary={() => void revoke()}
      >
        {link ? (
          <div className="stack">
            <label className="field">
              链接
              <input
                value={link.url}
                readOnly
                onFocus={(event) => event.target.select()}
              />
            </label>
            <p>有效期至 {new Date(link.expiresAtMs).toLocaleString("zh-CN")}</p>
          </div>
        ) : null}
      </ConfirmDialog>
    </>
  );
}
