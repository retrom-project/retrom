import { useHorizontalWheel } from "@/lib/use-horizontal-wheel";
import type { Schema } from "@/lib/api/types";
import { BrowserTime } from "@/components/browser-time";
export function UserResults({
  data,
  offset,
  onPage,
  onEdit,
}: {
  data: Schema<"UserPage">;
  offset: number;
  onPage: (offset: number) => void;
  onEdit: (user: Schema<"User">) => void;
}) {
  return (
    <>
      <UserTable users={data.items} onEdit={onEdit} />
      {!data.items.length ? (
        <p className="panel compact-empty">当前筛选下没有用户。</p>
      ) : null}
      {data.total > 24 ? (
        <div className="library-pagination">
          <button
            className="button secondary"
            disabled={offset === 0}
            onClick={() => onPage(offset - 24)}
          >
            上一页
          </button>
          <span>{data.total} 位用户</span>
          <button
            className="button secondary"
            disabled={offset + data.items.length >= data.total}
            onClick={() => onPage(offset + 24)}
          >
            下一页
          </button>
        </div>
      ) : null}
    </>
  );
}
export function UserTable({
  users,
  onEdit,
}: {
  users: Schema<"User">[];
  onEdit: (user: Schema<"User">) => void;
}) {
  const tableRail = useHorizontalWheel<HTMLDivElement>();
  return (
    <section className="panel user-table-panel">
      <div ref={tableRail} className="user-table-wrap">
        <table className="user-table">
          <thead>
            <tr>
              <th>用户</th>
              <th>角色</th>
              <th>状态</th>
              <th>最近登录</th>
              <th>创建时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {users.map((user) => (
              <tr key={user.id}>
                <td data-label="用户">
                  <strong>{user.displayName}</strong>
                  <small>@{user.username}</small>
                </td>
                <td data-label="角色">{user.role === "admin" ? "管理员" : "普通用户"}</td>
                <td data-label="状态">
                  <span
                    className={`status ${user.status === "active" ? "good" : "neutral"}`}
                  >
                    {statusLabels[user.status]}
                  </span>
                </td>
                <td data-label="最近登录">
                  {user.lastLoginAtMs ? (
                    <BrowserTime value={user.lastLoginAtMs} />
                  ) : (
                    "从未登录"
                  )}
                </td>
                <td data-label="创建时间">
                  <BrowserTime value={user.createdAtMs} />
                </td>
                <td data-label="操作">
                  <div className="workspace-actions">
                    <button
                      className="button secondary"
                      onClick={() => onEdit(user)}
                    >
                      管理
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
const statusLabels = { active: "启用", disabled: "停用", deleted: "已删除" };
