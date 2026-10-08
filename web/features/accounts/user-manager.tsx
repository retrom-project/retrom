"use client";
import { AccountLinkList } from "./account-link-list";
import { UserResults } from "./user-table";
import { UserEditor, InvitationForm, UserSearch } from "./user-forms";
import { useCallback, useState } from "react";
import { api, result, ApiError } from "@/lib/api/client";
import { useToast } from "@/components/toast-provider";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { PageHeader, FeedbackBanner } from "@/components/ui";
import { ResourceState } from "@/components/resource-state";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import { AccountLinkDialog } from "./user-link-dialog";
export function UserManager() {
  const { notify } = useToast();
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
  const [deleting, setDeleting] = useState<Schema<"User"> | null>(null);
  const [link, setLink] = useState<Schema<"AccountLink"> | null>(null);
  const [editing, setEditing] = useState<Schema<"User"> | null>(null);
  const [inviting, setInviting] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [linkRevision, setLinkRevision] = useState(0);
  const reportFailure = useAccountError(setError);
  function openEdit(user: Schema<"User">) {
    setError("");
    setEditing(user);
  }
  async function invitation(role: "admin" | "user", expiresInHours: number) {
    setBusy(true);
    setError("");
    try {
      setLink(
        result(
          await api.POST("/api/v1/admin/invitations", {
            body: { role, expiresInHours },
          }),
        ),
      );
      setInviting(false);
      notify({ tone: "good", message: "邀请链接已创建。" });
      setLinkRevision((value) => value + 1);
    } catch (failure) {
      reportFailure(failure, "邀请创建失败。");
    } finally {
      setBusy(false);
    }
  }
  async function reset(userId: string) {
    setBusy(true);
    setError("");
    try {
      setLink(
        result(
          await api.POST("/api/v1/admin/users/{userId}/password-reset-links", {
            params: { path: { userId } },
            body: { expiresInHours: 72 },
          }),
        ),
      );
      setEditing(null);
      notify({ tone: "good", message: "密码重置链接已创建。" });
      setLinkRevision((value) => value + 1);
    } catch (failure) {
      reportFailure(failure, "重置链接创建失败。");
    } finally {
      setBusy(false);
    }
  }
  async function save() {
    if (!editing) {
      return;
    }
    setBusy(true);
    setError("");
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
      notify({ tone: "good", message: "账号已更新。" });
    } catch (failure) {
      reportFailure(failure, "账号更新失败。");
    } finally {
      setBusy(false);
    }
  }
  async function removeUser() {
    if (!deleting) {
      return;
    }
    setBusy(true);
    setError("");
    try {
      result(
        await api.PATCH("/api/v1/admin/users/{userId}", {
          params: { path: { userId: deleting.id } },
          body: {
            version: deleting.version,
            displayName: deleting.displayName,
            role: deleting.role,
            status: "deleted",
          },
        }),
      );
      setDeleting(null);
      setEditing(null);
      users.reload();
      notify({ tone: "good", message: "账号已删除。" });
    } catch (failure) {
      reportFailure(failure, "删除账号失败。");
    } finally {
      setBusy(false);
    }
  }
  async function revoke() {
    if (!link) {
      return;
    }
    setBusy(true);
    setError("");
    try {
      const response = await api.DELETE(
        "/api/v1/admin/account-links/{accountLinkId}",
        {
          params: { path: { accountLinkId: link.id } },
          body: { version: link.version },
        },
      );
      if (response.error) {
        throw new ApiError(response.error.code, response.error.message, response.response.status);
      }
      setLink(null);
      notify({ tone: "good", message: "链接已撤销。" });
      setLinkRevision((value) => value + 1);
    } catch (failure) {
      reportFailure(failure, "撤销失败。");
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="user-admin-page">
      <PageHeader
        title="用户管理"
        description="创建邀请并管理谁可以登录；收藏、存档和最近游玩始终保持私有。"
        actions={
          <button
            className="button"
            onClick={() => {
              setError("");
              setInviting(true);
            }}
          >
            创建邀请
          </button>
        }
      />
      {error && !editing && !inviting && !link && !deleting ? (
        <FeedbackBanner tone="bad">{error}</FeedbackBanner>
      ) : null}
      <UserSearch
        onSearch={(value) => {
          setQ(value);
          setOffset(0);
        }}
      />
      <ResourceState resource={users}>
        {(data) => (
          <UserResults
            data={data}
            offset={offset}
            onPage={setOffset}
            onEdit={openEdit}
          />
        )}
      </ResourceState>
      <AccountLinkList key={linkRevision} />
      <ResponsiveSheet
        open={inviting}
        busy={busy}
        title="创建邀请"
        description="完整链接只会在创建成功后显示一次。"
        placement="right"
        className="user-management-sheet"
        onClose={() => setInviting(false)}
      >
        <InvitationForm
          busy={busy}
          error={error}
          onCancel={() => setInviting(false)}
          onSubmit={(role, hours) => void invitation(role, hours)}
        />
      </ResponsiveSheet>
      <ResponsiveSheet
        open={editing !== null && !deleting}
        busy={busy}
        title="管理用户"
        description="只管理账号与安全状态，不提供他人的私有游戏数据。"
        placement="right"
        className="user-management-sheet"
        onClose={() => setEditing(null)}
      >
        {editing ? (
          <UserEditor
            user={editing}
            busy={busy}
            error={error}
            onChange={setEditing}
            onSave={() => void save()}
            onReset={() => void reset(editing.id)}
            onDelete={() => {
              setError("");
              setDeleting(editing);
            }}
          />
        ) : null}
      </ResponsiveSheet>
      <ConfirmDialog
        open={!!deleting}
        title="删除账号"
        description="这个账号将无法登录，其私有游戏数据随后清理。"
        tone="danger"
        busy={busy}
        confirmLabel="删除账号"
        onCancel={() => setDeleting(null)}
        onConfirm={() => void removeUser()}
      >
        {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
      </ConfirmDialog>
      {link ? (
        <AccountLinkDialog
          key={link.id}
          link={link}
          busy={busy}
          error={error}
          onClose={() => setLink(null)}
          onRevoke={() => void revoke()}
        />
      ) : null}
    </div>
  );
}

function useAccountError(onValidation: (message: string) => void) {
  const { notify } = useToast();
  return (failure: unknown, fallback: string) => {
    const message = failure instanceof Error ? failure.message : fallback;
    if (failure instanceof ApiError && failure.status === 400) {
      onValidation(message);
    } else {
      notify({ tone: "bad", message });
    }
  };
}
