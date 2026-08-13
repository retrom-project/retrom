import { PageHeader } from "@/components/ui";

export default function NetplayRoomLoading() {
  return <div className="page-layout netplay-room-page" role="status" aria-live="polite">
    <span className="sr-only">正在加载联机房间状态</span>
    <PageHeader eyebrow="联机房间" title="正在同步房间状态" description="正在读取游戏、座位与参与者准备状态。" />
    <div className="netplay-seat-grid netplay-loading-seats" aria-hidden="true">
      {Array.from({ length: 4 }, (_, index) => <div className="netplay-seat" key={index}><strong>P{index + 1}</strong><span className="netplay-empty-seat" /><i /></div>)}
    </div>
  </div>;
}
