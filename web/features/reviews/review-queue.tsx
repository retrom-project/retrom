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

  return <section className="panel table-wrap" aria-label="待审核队列">
    <div className="panel-head"><div><h2>待审核队列</h2><p>当前已加载 {items.length} 条，可任意选择审核顺序</p></div><StatusBadge tone="info">{items.length}</StatusBadge></div>
    <table className="review-queue-table"><thead><tr><th>游戏 / 来源文件</th><th>目标目录</th><th>运行检查</th><th>信息候选</th><th>更新时间</th><th>操作</th></tr></thead><tbody>{items.map((item) => <tr key={item.itemId} data-review-item={item.itemId}><td><div className="review-source"><div className="review-thumb">{item.coverUrl ? <Image src={item.coverUrl} alt={`${item.draftTitle} 封面缩略图`} width={72} height={72} unoptimized /> : <span role="img" aria-label={`${item.draftTitle} 暂无封面`}>R</span>}</div><div><strong>{item.draftTitle}</strong><small className="review-source-name" title={item.sourceDisplayName}>{item.sourceDisplayName}</small><small className="review-file-facts"><span>{formatBytes(item.sourceTotalSizeBytes)}</span><code title={item.sourceMd5 ? `MD5 ${item.sourceMd5}` : "MD5 暂不可用"}>{item.sourceMd5 ? `MD5 ${item.sourceMd5}` : "MD5 暂不可用"}</code></small></div></div></td><td>{item.platformInstance.name}</td><td><StatusBadge tone={statusTone(item.blockerCodes[0] ?? item.validationStatus)}>{validationLabels[item.validationStatus] ?? item.validationStatus}{item.blockerCodes.length ? " · 需要处理" : ""}</StatusBadge></td><td>{item.candidateCount}</td><td>{formatTime(item.updatedAtMs)}</td><td><Link className="row-action" onClick={remember} href={`/admin/reviews/${item.itemId}?returnTo=${encodeURIComponent(listURL)}`}>审核条目</Link></td></tr>)}</tbody></table>
    <div className="queue-footer">{nextCursor ? <button type="button" className="button secondary" disabled={loading} onClick={() => void loadMore()}>{loading ? "正在加载…" : "加载更多待审条目"}</button> : <span className="status good">已加载当前筛选的全部条目</span>}{error ? <span role="alert" className="status bad">{error}</span> : null}</div>
  </section>;
}
