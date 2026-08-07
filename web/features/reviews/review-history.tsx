"use client";

import Image from "next/image";
import { useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { StatusBadge } from "@/components/ui";
import { formatTime } from "@/lib/backend";
import { responseError } from "@/lib/upload";

export type HistoryItem = {
  reviewEventId: string;
  importItemId: string;
  importJobId: string;
  title: string;
  decision: "APPROVED" | "DISCARDED";
  reason: string | null;
  createdAtMs: number;
};

type HistoryDetail = {
  reviewEventId: string;
  eventType: "APPROVED" | "DISCARDED";
  reason: string | null;
  createdAtMs: number;
  before: {
    metadata?: { title?: string; description?: string; developer?: string; publisher?: string; genre?: string; players?: number | null; releaseYear?: number | null };
    selectedAssets?: { coverCandidateAssetId?: string | null; coverUploadedAssetId?: string | null };
  };
};

const fields: Array<[string, keyof NonNullable<HistoryDetail["before"]["metadata"]>]> = [
  ["开发商", "developer"], ["发行商", "publisher"], ["类型", "genre"], ["玩家数", "players"], ["发行年份", "releaseYear"],
];

function HistoryCover({ assetId, title }: { assetId: string | null; title: string }) {
  const [failed, setFailed] = useState(false);
  if (!assetId) return <div className="asset-placeholder">当时未选择封面</div>;
  if (failed) return <div className="asset-placeholder">历史封面暂不可用</div>;
  return <Image src={`/api/v1/admin/review-assets/${assetId}`} alt={`${title} 审核时封面`} width={360} height={480} unoptimized onError={() => setFailed(true)} />;
}

export function ReviewHistory({ items }: { items: HistoryItem[] }) {
  const [selected, setSelected] = useState<HistoryItem | null>(null);
  const [detail, setDetail] = useState<HistoryDetail | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function open(item: HistoryItem) {
    setSelected(item); setDetail(null); setError(""); setLoading(true);
    try {
      const response = await fetch(`/api/v1/admin/review-history/${item.reviewEventId}`, { cache: "no-store" });
      if (!response.ok) throw new Error(await responseError(response, "无法读取审核快照"));
      setDetail(await response.json() as HistoryDetail);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "无法读取审核快照");
    } finally { setLoading(false); }
  }

  const metadata = detail?.before.metadata;
  const coverId = detail?.before.selectedAssets?.coverUploadedAssetId
    ?? detail?.before.selectedAssets?.coverCandidateAssetId
    ?? null;
  return <>
    <section className="panel table-wrap"><table className="history-table"><thead><tr><th>游戏</th><th>决定</th><th>原因</th><th>时间</th></tr></thead><tbody>{items.map((item) => <tr key={item.reviewEventId} role="button" tabIndex={0} aria-label={`查看“${item.title}”的审核快照`} onClick={() => void open(item)} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); void open(item); } }}><td><strong>{item.title}</strong><small>点击查看审核完成时的游戏信息</small></td><td><StatusBadge tone={item.decision === "APPROVED" ? "good" : "bad"}>{item.decision === "APPROVED" ? "已发布" : "已丢弃"}</StatusBadge></td><td>{item.reason ?? "—"}</td><td>{formatTime(item.createdAtMs)}</td></tr>)}</tbody></table></section>
    <ConfirmDialog open={selected !== null} wide hideCancel title="审核完成时的游戏信息" description={selected ? `${selected.decision === "APPROVED" ? "发布" : "丢弃"}于 ${formatTime(selected.createdAtMs)}` : undefined} confirmLabel="关闭" busy={loading} onCancel={() => setSelected(null)} onConfirm={() => setSelected(null)}>
      {loading ? <p className="scrape-live"><i className="button-spinner" aria-hidden="true" />正在读取当时的元信息…</p> : error ? <p role="alert">{error}</p> : <div className="history-snapshot"><div className="history-snapshot-cover"><HistoryCover key={coverId ?? selected?.reviewEventId} assetId={coverId} title={metadata?.title ?? selected?.title ?? "游戏"} /></div><div><h3>{metadata?.title ?? selected?.title ?? "未命名游戏"}</h3><p>{metadata?.description || "当时未填写简介。"}</p><dl>{fields.map(([label, key]) => <div key={key}><dt>{label}</dt><dd>{metadata?.[key] ?? "—"}</dd></div>)}</dl>{detail?.reason ? <p className="history-reason"><strong>审核说明</strong>{detail.reason}</p> : null}</div></div>}
    </ConfirmDialog>
  </>;
}
