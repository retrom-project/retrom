import Link from "next/link";
import { PageHeader } from "@/components/ui";
import { ScanProgressList } from "@/features/scans/scan-progress";
export default function Page() {
  return (
    <>
      <PageHeader
        title="游戏入库"
        description="扫描服务器目录中的游戏或 BIOS，并在待审核入口发布游戏。"
      />
      <div className="admin-grid">
        <article className="admin-card">
          <h2>来源扫描</h2>
          <p>游戏扫描与 BIOS 扫描</p>
          <Link className="button" href="/admin/imports/server">
            开始扫描
          </Link>
        </article>
        <article className="admin-card">
          <h2>待审核</h2>
          <p>编辑资料、试玩并批准入库</p>
          <Link className="button secondary" href="/admin/reviews">
            进入待审核
          </Link>
        </article>
      </div>
      <ScanProgressList />
    </>
  );
}
