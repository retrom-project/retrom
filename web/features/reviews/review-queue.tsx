"use client";

import Link from "next/link";
import Image from "next/image";
import { useEffect, useMemo, useRef, useState } from "react";
import { StatusBadge } from "@/components/ui";
import { formatTime, type ListResponse } from "@/lib/backend";
import { statusTone } from "@/lib/status";

export type ReviewQueueItem = {
  itemId: string;
  reviewVersion: number;
  importJobId: string;
  sourceDisplayName: string;
  draftTitle: string;
  platformInstance: { id: string; name: string };
  validationStatus: string;
  validationJobId: string | null;
  blockerCodes: string[];
  candidateCount: number;
  sourceTotalSizeBytes: number;
  sourceMd5: string | null;
  coverUrl: string | null;
  updatedAtMs: number;
};

function queryString(values: Record<string, string>) {
  return new URLSearchParams(Object.entries(values).filter(([, value]) => value)).toString();
}

const validationLabels: Record<string, string> = { READY: "可以发布", BLOCKED: "缺少依赖", DEPENDENCY_MISSING: "缺少依赖", INCOMPATIBLE: "不兼容", NEEDS_VALIDATION: "等待检查" };

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KiB`;
  if (value < 1024 ** 3) return `${(value / 1024 ** 2).toFixed(1)} MiB`;
  return `${(value / 1024 ** 3).toFixed(2)} GiB`;
}

export function ReviewQueue({ initial, values }: { initial: ListResponse<ReviewQueueItem>; values: Record<string, string> }) {
  const listQuery = useMemo(() => queryString(values), [values]);
  const listURL = `/admin/reviews${listQuery ? `?${listQuery}` : ""}`;
  const storageKey = `retrom:review-queue:${listQuery}`;
  const [items, setItems] = useState(initial.items);
  const [nextCursor, setNextCursor] = useState(initial.nextCursor);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const persistenceReady = useRef(false);

  useEffect(() => {
    const raw = sessionStorage.getItem(storageKey);
    if (!raw) { persistenceReady.current = true; return; }
    try {
      const saved = JSON.parse(raw) as { items: ReviewQueueItem[]; nextCursor: string | null; scrollY: number };
      if (saved.items.length >= initial.items.length) {
        const frame = requestAnimationFrame(() => {
          setItems(saved.items);
          setNextCursor(saved.nextCursor);
          persistenceReady.current = true;
          requestAnimationFrame(() => window.scrollTo({ top: saved.scrollY }));
        });
        return () => cancelAnimationFrame(frame);
      }
    } catch {
      sessionStorage.removeItem(storageKey);
    }
    persistenceReady.current = true;
  }, [initial.items.length, storageKey]);

  useEffect(() => {
    if (!persistenceReady.current) return;
    sessionStorage.setItem(storageKey, JSON.stringify({ items, nextCursor, scrollY: window.scrollY }));
  }, [items, nextCursor, storageKey]);

  function remember() {
    sessionStorage.setItem(storageKey, JSON.stringify({ items, nextCursor, scrollY: window.scrollY }));
  }

  async function loadMore() {
    if (!nextCursor || loading) return;
    setLoading(true);
    setError("");
    try {
      const query = new URLSearchParams(values);
      query.set("cursor", nextCursor);
      query.set("limit", "50");
      const response = await fetch(`/api/v1/admin/reviews?${query}`, { cache: "no-store" });
      if (!response.ok) throw new Error("无法加载下一页待审条目");
      const page = await response.json() as ListResponse<ReviewQueueItem>;
      setItems((current) => {
        const seen = new Set(current.map((item) => item.itemId));
        return [...current, ...page.items.filter((item) => !seen.has(item.itemId))];
      });
      setNextCursor(page.nextCursor);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "无法加载下一页待审条目");
    } finally {
      setLoading(false);
    }
  }

  return <section className="review-workflow-queue" aria-label="待审核队列">
    <div className="review-workflow-list">{items.map((item) => <article className="review-workflow-row" key={item.itemId} data-review-item={item.itemId}>
      <div className="review-workflow-game"><div className="review-workflow-thumb">{item.coverUrl ? <Image src={item.coverUrl} alt={`${item.draftTitle} 封面缩略图`} width={56} height={56} unoptimized /> : <span role="img" aria-label={`${item.draftTitle} 暂无封面`}>{item.draftTitle.slice(0, 2).toUpperCase() || "R"}</span>}</div><div><h3>{item.draftTitle}</h3><p title={item.sourceDisplayName}><span>{item.sourceDisplayName}</span> · <span>{formatBytes(item.sourceTotalSizeBytes)}</span> · <code title={item.sourceMd5 ? `MD5 ${item.sourceMd5}` : "MD5 暂不可用"}>{item.sourceMd5 ? `MD5 ${item.sourceMd5.slice(0, 4)}…` : "MD5 暂不可用"}</code></p></div></div>
      <div className="review-workflow-directory">{item.platformInstance.name}</div>
      <StatusBadge tone={statusTone(item.blockerCodes[0] ?? item.validationStatus)}>{validationLabels[item.validationStatus] ?? item.validationStatus}{item.blockerCodes.length ? " · 需要处理" : ""}</StatusBadge>
      <div className="review-workflow-candidate"><strong>{item.candidateCount ? "已找到游戏信息" : "未找到游戏信息"}</strong><small>{item.candidateCount ? `${item.candidateCount} 个候选` : "需要手动填写"}</small></div>
      <div className="review-workflow-wait"><strong>{formatTime(item.updatedAtMs)}</strong><small>更新时间</small></div>
      <Link aria-label={item.validationStatus === "READY" && !item.blockerCodes.length ? "审核条目" : "处理条目"} className={item.validationStatus === "READY" && !item.blockerCodes.length ? "button" : "button secondary"} onClick={remember} href={`/admin/reviews/${item.itemId}?returnTo=${encodeURIComponent(listURL)}`}>{item.validationStatus === "READY" && !item.blockerCodes.length ? "审核" : "处理"}</Link>
    </article>)}</div>
    <div className="queue-footer"><span>当前已加载 {items.length} 条，可任意选择审核顺序</span>{nextCursor ? <button type="button" className="button secondary" disabled={loading} onClick={() => void loadMore()}>{loading ? "正在加载…" : "加载更多待审条目"}</button> : <span className="status good"><i />已加载当前筛选的全部条目</span>}{error ? <span role="alert" className="status bad"><i />{error}</span> : null}</div>
  </section>;
}
