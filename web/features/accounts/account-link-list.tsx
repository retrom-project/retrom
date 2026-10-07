"use client";
import { useCallback, useState } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { ResourceState } from "@/components/resource-state";
import { BrowserTime } from "@/components/browser-time";
import { ConfirmDialog } from "@/components/confirm-dialog";
import type { Schema } from "@/lib/api/types";
export function AccountLinkList() {
  const [status, setStatus] = useState<
    Schema<"AccountLinkSummary">["status"] | ""
  >("active");
  const [offset, setOffset] = useState(0);
  const [target, setTarget] = useState<Schema<"AccountLinkSummary"> | null>(
    null,
  );
  const [error, setError] = useState("");
  const loader = useCallback(
    async () =>
      result(
        await api.GET("/api/v1/admin/account-links", {
          params: { query: { offset, limit: 24, status: status || undefined } },
        }),
      ),
    [offset, status],
  );
  const links = useResource(loader);
  async function revoke() {
    if (!target) {
      return;
    }
    const response = await api.DELETE(
      "/api/v1/admin/account-links/{accountLinkId}",
      {
        params: { path: { accountLinkId: target.id } },
        body: { version: target.version },
      },
    );
    if (response.error) {
      setError(response.error.message);
      return;
    }
    setTarget(null);
    links.reload();
  }
  return (
    <section className="panel invitation-list">
      <header className="workspace-row">
        <h2>邀请与密码重置链接</h2>
        <label className="field">
          状态
          <select
            value={status}
            onChange={(event) => {
              setStatus(event.target.value as typeof status);
              setOffset(0);
            }}
          >
            <option value="">全部状态</option>
            {Object.entries(statusLabels).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
      </header>
      <ResourceState resource={links}>
        {(data) => (
          <>
            <div className="user-table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>用途</th>
                    <th>角色</th>
                    <th>状态</th>
                    <th>创建时间</th>
                    <th>有效期</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {data.items.map((link) => (
                    <tr key={link.id}>
                      <td>
                        {link.kind === "invitation" ? "邀请" : "密码重置"}
                      </td>
                      <td>
                        {link.role === "admin"
                          ? "管理员"
                          : link.role === "user"
                            ? "普通用户"
                            : "—"}
                      </td>
                      <td>{statusLabels[link.status]}</td>
                      <td>
                        <BrowserTime value={link.createdAtMs} />
                      </td>
                      <td>
                        <BrowserTime value={link.expiresAtMs} />
                      </td>
                      <td>
                        <button
                          className="button secondary"
                          disabled={link.status !== "active"}
                          onClick={() => setTarget(link)}
                        >
                          撤销
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {!data.items.length ? (
              <p className="compact-empty">当前筛选下没有链接。</p>
            ) : null}
            <div className="library-pagination">
              <button
                className="button secondary"
                disabled={offset === 0}
                onClick={() => setOffset(offset - 24)}
              >
                上一页
              </button>
              <span>{data.total} 条</span>
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
      <ConfirmDialog
        open={!!target}
        title="撤销链接"
        description="撤销后，这份邀请或密码重置链接将立即失效。"
        onCancel={() => setTarget(null)}
        onConfirm={() => void revoke()}
      >
        {error ? <p role="alert">{error}</p> : null}
      </ConfirmDialog>
    </section>
  );
}
const statusLabels: Record<Schema<"AccountLinkSummary">["status"], string> = {
  active: "待使用",
  consumed: "已使用",
  revoked: "已撤销",
  expired: "已过期",
};
