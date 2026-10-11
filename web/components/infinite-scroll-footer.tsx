"use client";
import { useEffect, useRef, type RefObject } from "react";

export function InfiniteScrollFooter({ count, total, hasMore, loadingMore, moreError, loadMore, scrollRoot }: {
  count: number;
  total: number;
  hasMore: boolean;
  loadingMore: boolean;
  moreError: string;
  loadMore: () => void;
  scrollRoot?: RefObject<HTMLElement | null>;
}) {
  const marker = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const element = marker.current;
    if (!element || !hasMore || loadingMore || moreError || !globalThis.IntersectionObserver) { return; }
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) { loadMore(); }
    }, { root: scrollRoot?.current, rootMargin: "0px 0px 240px 0px" });
    observer.observe(element);
    return () => observer.disconnect();
  }, [count, hasMore, loadingMore, moreError, loadMore, scrollRoot]);
  return <div className="workspace-paging" ref={marker} aria-label="列表加载进度">
    <span role="status">已加载 {count} / {total}{loadingMore ? " · 正在加载…" : !hasMore && !moreError ? " · 已加载全部" : ""}</span>
    {moreError ? <><span role="alert">{moreError}</span><button type="button" className="button secondary" onClick={loadMore}>重试加载</button></>
      : hasMore ? <button type="button" className="button secondary" disabled={loadingMore} onClick={loadMore}>加载更多</button> : null}
  </div>;
}
