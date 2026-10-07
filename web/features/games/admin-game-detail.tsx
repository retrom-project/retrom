"use client";
import Link from "next/link";
import type { Schema } from "@/lib/api/types";
import { BrowserTime } from "@/components/browser-time";
import { PageHeader } from "@/components/ui";
import { LaunchButton } from "@/features/player/launch-button";
import { AdminGameCover } from "./admin-game-browser";
import { GameEditor } from "./game-editor";
import { GameManagement } from "./game-management";
import { ReviewBiosStatus } from "./review-bios-status";
export function AdminGameDetail({ detail, mode, busy, onReview, onChange }: {
  detail: Schema<"GameDetail">;
  mode: "admin" | "review";
  busy: boolean;
  onReview: (action: "approve" | "discard") => void;
  onChange: () => void;
}) {
  const { game } = detail;
  const review = mode === "review";
  return <div className="admin-detail-page">
    <PageHeader
      title={review ? "审核条目" : game.title}
      description={review ? "核对资料与运行配置；修改后请先保存，再通过并发布。" : "维护发布信息、媒体与标签，查看游戏文件及管理操作。"}
      actions={<Link className="button secondary" href={review ? "/admin/reviews" : "/admin/games"}>{review ? "返回待审核列表" : "返回游戏管理"}</Link>}
    />
    <div className={`admin-game-detail${review ? " review-game-detail" : ""}`}>
      {review ? <ReviewOverview detail={detail} busy={busy} onReview={onReview} /> : <>
        <section className="admin-game-hero">
          <AdminGameCover game={game} />
          <div className="admin-game-hero-copy"><h2>{game.title}</h2><p>{game.platformId} · {game.directoryName}{game.developer ? ` · ${game.developer}` : ""}</p><div><span className="status good">用户可见</span><span className="status neutral">{game.media.some((media) => media.kind === "cover") ? "已设置封面" : "暂无封面"}</span></div></div>
          <div className="admin-game-hero-update"><span>最近更新</span><strong><BrowserTime value={game.updatedAtMs} /></strong><small>{detail.files.length} 个内容文件 · {detail.coreIds.length} 种运行方式</small></div>
        </section>
        <section className="admin-game-overview" aria-label="游戏概览">
          <div><span>所属目录</span><strong>{game.directoryName}</strong></div>
          <div><span>推荐运行方式</span><strong>{detail.defaultCoreId || "—"}</strong></div>
          <div><span>当前游戏文件</span><strong title={detail.files[0]?.logicalKey}>{detail.files[0]?.logicalKey ?? "—"}</strong></div>
          <div><span>我的存档</span><strong>{detail.saves.length} 份</strong></div>
        </section>
      </>}
      <GameEditor key={game.version} detail={detail} mode={mode} onSaved={onChange} />
      {!review ? <GameManagement detail={detail} onChange={onChange} /> : null}
    </div>
  </div>;
}
function ReviewOverview({ detail, busy, onReview }: {
  detail: Schema<"GameDetail">;
  busy: boolean;
  onReview: (action: "approve" | "discard") => void;
}) {
  return <>
    <section className="review-summary" aria-label="审核条目概览">
      <AdminGameCover game={detail.game} />
      <div className="review-summary-copy">
        <h2>{detail.game.title}</h2>
        <p>{detail.game.directoryName} · {detail.game.platformId}</p>
      </div>
      <div className="review-summary-actions">
        <LaunchButton gameId={detail.game.id} coreId={detail.defaultCoreId} purpose="review" variant="secondary" disabled={busy}>运行游戏</LaunchButton>
        <button className="button secondary" disabled={busy} onClick={() => onReview("discard")}>丢弃条目</button>
        <button className="button" disabled={busy} onClick={() => onReview("approve")}>通过并发布</button>
      </div>
    </section>
    <ReviewBiosStatus detail={detail} />
  </>;
}
