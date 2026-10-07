import type { Schema } from "@/lib/api/types";
import { BrowserTime } from "@/components/browser-time";
export function UserTable({
  users,
  onEdit,
  onReset,
}: {
  users: Schema<"User">[];
  onEdit: (user: Schema<"User">) => void;
  onReset: (userId: string) => void;
}) {
  return (
    <section className="panel user-table-panel">
      <div className="user-table-wrap">
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
                <td>
                  <strong>{user.displayName}</strong>
                  <small>@{user.username}</small>
                </td>
                <td>{user.role === "admin" ? "管理员" : "普通用户"}</td>
                <td>
                  <span
                    className={`status ${user.status === "active" ? "good" : "neutral"}`}
                  >
                    {statusLabels[user.status]}
                  </span>
                </td>
                <td>
                  {user.lastLoginAtMs ? (
                    <BrowserTime value={user.lastLoginAtMs} />
                  ) : (
                    "从未登录"
                  )}
                </td>
                <td>
                  <BrowserTime value={user.createdAtMs} />
                </td>
                <td>
                  <div className="workspace-actions">
                    <button
                      className="button secondary"
                      onClick={() => onEdit(user)}
                    >
                      管理
                    </button>
                    <button
                      className="button secondary"
                      onClick={() => onReset(user.id)}
                    >
                      密码重置链接
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
