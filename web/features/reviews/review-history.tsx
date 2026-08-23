"use client";

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
  };
};

const fields: Array<[string, keyof NonNullable<HistoryDetail["before"]["metadata"]>]> = [
  ["开发商", "developer"], ["发行商", "publisher"], ["类型", "genre"], ["玩家数", "players"], ["发行年份", "releaseYear"],
];

export function ReviewHistory({ items }: { items: HistoryItem[] }) {
  const [selected, setSelected] = useState<HistoryItem | null>(null);
  const [detail, setDetail] = useState<HistoryDetail | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  async function open(item: HistoryItem) {
    setSelected(item); setDetail(null); setError(""); setLoading(true);
    try {
      const response = await fetch(`/api/v1/admin/review-history/${item.reviewEventId}`, { cache: "no-store" });
      if (!response.ok) {throw new Error(await responseError(response, "无法读取审核快照"));}
      setDetail(await response.json() as HistoryDetail);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "无法读取审核快照");
    } finally { setLoading(false); }
  }

  const metadata = detail?.before.metadata;
  return <>
    <HistoryList items={items} onOpen={(item) => void open(item)} />
    <HistoryDialog {...{ detail, error, loading, metadata, selected }} onClose={() => setSelected(null)} />
  </>;
}

function HistoryList({ items, onOpen }: { items: HistoryItem[]; onOpen: (item: HistoryItem) => void }) {
  return <section className="review-history-list" aria-label="审核历史">{items.map((item) => {
    const approved = item.decision === "APPROVED";
    const activate = () => onOpen(item);
    return <article key={item.reviewEventId} role="button" tabIndex={0} aria-label={`查看“${item.title}”的审核快照`} onClick={activate} onKeyDown={(event) => {if (event.key === "Enter" || event.key === " ") {event.preventDefault(); activate();}}}><div><h3>{item.title}</h3><small>审核完成时的信息快照</small></div><StatusBadge tone={approved ? "good" : "bad"}>{approved ? "已发布" : "已丢弃"}</StatusBadge><span>{item.reason ?? (approved ? "审核通过并发布" : "管理员丢弃条目")}</span><time>{formatTime(item.createdAtMs)}</time><span className="button secondary">查看快照</span></article>;
  })}</section>;
}

function HistoryDialog({ detail, error, loading, metadata, onClose, selected }: {
  detail: HistoryDetail | null;
  error: string;
  loading: boolean;
  metadata: HistoryDetail["before"]["metadata"];
  onClose: () => void;
  selected: HistoryItem | null;
}) {
  const decision = selected?.decision === "APPROVED" ? "发布" : "丢弃";
  const description = selected ? `审核完成时的决策快照 · ${decision}于 ${formatTime(selected.createdAtMs)}` : undefined;
  return <ConfirmDialog open={selected !== null} wide hideCancel title={selected?.title ?? "审核完成时的游戏信息"} description={description} confirmLabel="关闭" busy={loading} onCancel={onClose} onConfirm={onClose}>
    <HistoryDialogContents {...{ detail, error, loading, metadata, selected }} />
  </ConfirmDialog>;
}

function HistoryDialogContents({ detail, error, loading, metadata, selected }: {
  detail: HistoryDetail | null;
  error: string;
  loading: boolean;
  metadata: HistoryDetail["before"]["metadata"];
  selected: HistoryItem | null;
}) {
  if (loading) {return <p className="scrape-live"><i className="button-spinner" aria-hidden="true" />正在读取当时的元信息…</p>;}
  if (error) {return <p role="alert">{error}</p>;}
  return <div className="history-snapshot">
    <HistorySnapshotDetails {...{ detail, metadata, selected }} />
  </div>;
}

function HistorySnapshotDetails({ detail, metadata, selected }: {
  detail: HistoryDetail | null;
  metadata: HistoryDetail["before"]["metadata"];
  selected: HistoryItem | null;
}) {
  const approved = selected?.decision === "APPROVED";
  const title = metadata?.title ?? selected?.title ?? "未命名游戏";
  return <div>
    <StatusBadge tone={approved ? "good" : "bad"}>{approved ? "已发布" : "已丢弃"}</StatusBadge>
    <h3>{title}</h3>
    <p>{metadata?.description || "当时未填写简介。"}</p>
    <dl>{fields.map(([label, key]) => <div key={key}><dt>{label}</dt><dd>{metadata?.[key] ?? "—"}</dd></div>)}</dl>
    {detail?.reason ? <p className="history-reason"><strong>审核说明</strong>{detail.reason}</p> : null}
  </div>;
}
