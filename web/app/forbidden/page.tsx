import Link from "next/link";

export default function ForbiddenPage() {
  return (
    <main className="auth-route-message">
      <span aria-hidden="true">403</span>
      <h1>没有管理权限</h1>
      <p>用户管理仅包含账号与安全状态；游玩记录和存档保持私有。</p>
      <Link className="button" href="/">返回首页</Link>
    </main>
  );
}
