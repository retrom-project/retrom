import Link from "next/link";
export default function NotFound() {
  return (
    <div className="empty">
      <h1>页面不存在</h1>
      <Link className="button" href="/">
        返回首页
      </Link>
    </div>
  );
}
