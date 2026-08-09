"use client";

import Link from "next/link";
import Image from "next/image";
import { useEffect, useMemo, useRef, useState } from "react";
import { StatusBadge } from "@/components/ui";
import { formatTime, type ListResponse } from "@/lib/backend";
import { statusTone } from "@/lib/status";
import { useAuth } from "@/features/auth/auth-provider";
import { userStorageKey } from "@/features/auth/storage";

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
  const { context } = useAuth();
  const listQuery = useMemo(() => queryString(values), [values]);
  const listURL = `/admin/reviews${listQuery ? `?${listQuery}` : ""}`;
  const storageKey = userStorageKey(context.user?.userId, "reviews", `queue:${listQuery}`);
  const [items, setItems] = useState(initial.items);
  const [nextCursor, setNextCursor] = useState(initial.nextCursor);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [summaryFilter, setSummaryFilter] = useState<"ALL" | "READY" | "ABNORMAL" | "MISSING">("ALL");
  const persistenceReady = useRef(false);
  const loadMoreRef = useRef<HTMLDivElement>(null);
  const counts = useMemo(() => ({
    ready: items.filter((item) => item.validationStatus === "READY" && item.blockerCodes.length === 0).length,
    abnormal: items.filter((item) => item.validationStatus !== "READY" || item.blockerCodes.length > 0).length,
    missing: items.filter((item) => item.candidateCount === 0).length,
  }), [items]);
  const visibleItems = useMemo(() => items.filter((item) => {
    if (summaryFilter === "READY") return item.validationStatus === "READY" && item.blockerCodes.length === 0;
    if (summaryFilter === "ABNORMAL") return item.validationStatus !== "READY" || item.blockerCodes.length > 0;
    if (summaryFilter === "MISSING") return item.candidateCount === 0;
    return true;
  }), [items, summaryFilter]);

  useEffect(() => {
    if (!storageKey) { persistenceReady.current = true; return; }
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
    if (!persistenceReady.current || !storageKey) return;
    sessionStorage.setItem(storageKey, JSON.stringify({ items, nextCursor, scrollY: window.scrollY }));
  }, [items, nextCursor, storageKey]);

  function remember() {
    if (!storageKey) return;
    sessionStorage.setItem(storageKey, JSON.stringify({ items, nextCursor, scrollY: window.scrollY }));
  }

  async function loadMore() {
    if (!nextCursor || loading) return;
    setLoading(true);
    setError("");
    try {
      const query = new URLSearchParams(values);
      query.set("cursor", nextCursor);
      query.set("limit", "20");
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

  useEffect(() => {
    const target = loadMoreRef.current;
    if (!target || !nextCursor || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) void loadMore();
    }, { rootMargin: "320px 0px" });
    observer.observe(target);
    return () => observer.disconnect();
  });

  return <section className="review-workflow-queue" aria-label="待审核队列">
    <div className="import-workflow-chips review-workflow-chips" aria-label="筛选已加载的审核条目">
      <button type="button" className={summaryFilter === "ALL" ? "is-active" : ""} onClick={() => setSummaryFilter("ALL")}>当前已加载 {items.length}</button>
      <button type="button" className={summaryFilter === "READY" ? "is-active" : ""} onClick={() => setSummaryFilter("READY")}>可以发布 {counts.ready}</button>
      <button type="button" className={summaryFilter === "ABNORMAL" ? "is-active" : ""} onClick={() => setSummaryFilter("ABNORMAL")}>运行异常 {counts.abnormal}</button>
      <button type="button" className={summaryFilter === "MISSING" ? "is-active" : ""} onClick={() => setSummaryFilter("MISSING")}>未找到信息 {counts.missing}</button>
    </div>
    <div className="review-workflow-list">{visibleItems.map((item) => <article className="review-workflow-row" key={item.itemId} data-review-item={item.itemId}>
      <div className="review-workflow-game"><div className="review-workflow-thumb">{item.coverUrl ? <Image src={item.coverUrl} alt={`${item.draftTitle} 封面缩略图`} width={56} height={56} unoptimized /> : <span role="img" aria-label={`${item.draftTitle} 暂无封面`}>{item.draftTitle.slice(0, 2).toUpperCase() || "R"}</span>}</div><div><h3>{item.draftTitle}</h3><p title={item.sourceDisplayName}><span>{item.sourceDisplayName}</span> · <span>{formatBytes(item.sourceTotalSizeBytes)}</span> · <code title={item.sourceMd5 ? `MD5 ${item.sourceMd5}` : "MD5 暂不可用"}>{item.sourceMd5 ? `MD5 ${item.sourceMd5.slice(0, 4)}…` : "MD5 暂不可用"}</code></p></div></div>
      <div className="review-workflow-directory">{item.platformInstance.name}</div>
      <StatusBadge tone={statusTone(item.blockerCodes[0] ?? item.validationStatus)}>{validationLabels[item.validationStatus] ?? item.validationStatus}{item.blockerCodes.length ? " · 需要处理" : ""}</StatusBadge>
      <div className="review-workflow-candidate"><strong>{item.candidateCount ? "已找到游戏信息" : "未找到游戏信息"}</strong><small>{item.candidateCount ? `${item.candidateCount} 个候选` : "需要手动填写"}</small></div>
      <div className="review-workflow-wait"><strong>{formatTime(item.updatedAtMs)}</strong><small>更新时间</small></div>
      <Link aria-label={item.validationStatus === "READY" && !item.blockerCodes.length ? "审核条目" : "处理条目"} className={item.validationStatus === "READY" && !item.blockerCodes.length ? "button" : "button secondary"} onClick={remember} href={`/admin/reviews/${item.itemId}?returnTo=${encodeURIComponent(listURL)}`}>{item.validationStatus === "READY" && !item.blockerCodes.length ? "审核" : "处理"}</Link>
    </article>)}</div>
    {!visibleItems.length ? <div className="import-workflow-empty"><h2>已加载条目中没有匹配项</h2><p>继续向下滚动会加载下一页，或切换上方筛选。</p></div> : null}
    <div ref={loadMoreRef} className="infinite-scroll-sentinel" aria-hidden="true" />
    <div className="queue-footer"><span>当前筛选显示 {visibleItems.length} / 已加载 {items.length} 条</span>{nextCursor ? <button type="button" className="button secondary" disabled={loading} onClick={() => void loadMore()}>{loading ? "正在加载下一页…" : "继续加载"}</button> : <span className="status good"><i />已加载当前搜索条件的全部条目</span>}{error ? <span role="alert" className="status bad"><i />{error}</span> : null}</div>
  </section>;
}
