"use client";
import { useState } from "react";
import type { Schema } from "@/lib/api/types";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { LaunchButton } from "@/features/player/launch-button";
import { AdminGameCover } from "./admin-game-browser";
import type { ReviewCheck } from "./review-bios-status";

export function ReviewSummary({ detail, coreId, busy, onReview, check }: {
  detail: Schema<"GameDetail">;
  coreId: string;
  busy: boolean;
  onReview: (action: "approve" | "discard", coreId?: string) => void;
  check: ReviewCheck;
}) {
  const [confirming, setConfirming] = useState(false);
  function approve() {
    if (busy || !check.approvable) { return; }
    if (check.warnings.length) { setConfirming(true); }
    else { onReview("approve", coreId); }
  }
  return <>
    <section className="review-summary" aria-label="审核条目概览">
      <AdminGameCover game={detail.game} />
      <div className="review-summary-copy">
        <h2>{detail.game.title}</h2>
        <p>{detail.game.directoryName} · {detail.game.platformId}</p>
      </div>
      <div className="review-summary-actions">
        <LaunchButton gameId={detail.game.id} coreId={coreId} purpose="review" variant="secondary" disabled={busy}>运行游戏</LaunchButton>
        <button className="button secondary" disabled={busy} onClick={() => onReview("discard")}>丢弃条目</button>
        <button className="button" disabled={busy || !check.approvable} onClick={approve}
          title={!check.approvable ? "请先完成下方运行条件检查并修复阻断问题" : undefined}>
          {check.loading ? "正在检查…" : "通过并发布"}
        </button>
      </div>
    </section>
    <ConfirmDialog open={confirming} title="确认带风险发布？"
      description="文件 CRC 与当前核心的 DAT 不一致，游戏可能无法启动或运行异常。建议先测试运行，确认可以正常游玩后再发布；测试运行不是强制条件。"
      cancelLabel="返回检查" confirmLabel="确认风险并发布" busy={busy} confirmDisabled={!check.approvable}
      onCancel={() => setConfirming(false)} onConfirm={() => {
        if (!busy && check.approvable) { onReview("approve", coreId); setConfirming(false); }
      }} portalToBody>
      <div className="review-approval-risk">
        <p>检测到 {check.warnings.length} 项 CRC 差异，一键快速审核不会自动放行。发布后，其他用户将可以运行此游戏。</p>
        <ul>{(["ROM", "PARENT", "BIOS"] as const).map(role => {
          const count = check.warnings.filter(warning => warning.role === role).length;
          return count ? <li key={role}>{role === "PARENT" ? "Parent ROM" : role}：{count} 项</li> : null;
        })}</ul>
        <LaunchButton gameId={detail.game.id} coreId={coreId} purpose="review" variant="secondary" disabled={busy}>测试运行</LaunchButton>
      </div>
    </ConfirmDialog>
  </>;
}
