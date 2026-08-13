import { PageHeader } from "@/components/ui";

export default function NetplayLoading() {
  return <div className="page-layout netplay-page" role="status" aria-live="polite">
    <span className="sr-only">正在加载联机房间</span>
    <PageHeader eyebrow="NETPLAY" title="联机游玩" description="正在读取当前房间与最近联机记录。" />
    <div className="netplay-room-section netplay-loading-grid" aria-hidden="true">
      {Array.from({ length: 3 }, (_, index) => <div className="netplay-room-card netplay-loading-card" key={index}><i /><span /><span /></div>)}
    </div>
  </div>;
}
