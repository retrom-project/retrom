"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "@/features/auth/auth-provider";

export function useCursorPage<T extends { nextCursor: string | null }>(initialPage: T, url: string) {
  const { authenticatedFetch } = useAuth();
  const [page, setPage] = useState(initialPage);
  const [index, setIndex] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const cursors = useRef<Array<string | null>>([null]);
  const observedUrl = useRef(url);
  const loadedUrl = useRef(url);
  const controller = useRef<AbortController | null>(null);
  const generation = useRef(0);
  const failedRequest = useRef<{url: string; cursor: string | null; index: number} | null>(null);

  const requestPage = useCallback(async (requestedUrl: string, cursor: string | null, nextIndex: number) => {
    controller.current?.abort();
    const abort = new AbortController();
    controller.current = abort;
    const version = ++generation.current;
    failedRequest.current = null;
    setLoading(true);
    setError(null);
    try {
      const response = await authenticatedFetch(`${requestedUrl}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, { signal: abort.signal, cache: "no-store" });
      if (!response.ok) {throw new Error("暂时无法读取列表，请稍后重试");}
      const result = await response.json() as T;
      if (version !== generation.current) {return;}
      setPage((current) => ({ ...current, ...result }));
      loadedUrl.current = requestedUrl;
      setIndex(nextIndex);
      cursors.current[nextIndex] = cursor;
    } catch (caught) {
      if (version !== generation.current || abort.signal.aborted) {return;}
      failedRequest.current = {url: requestedUrl, cursor, index: nextIndex};
      setError(caught instanceof Error ? caught.message : "暂时无法读取列表");
    } finally {
      if (version === generation.current) {controller.current = null; setLoading(false);}
    }
  }, [authenticatedFetch]);

  useEffect(() => {
    if (observedUrl.current === url) {return;}
    observedUrl.current = url;
    controller.current?.abort();
    ++generation.current;
    failedRequest.current = null;
    cursors.current = [null];
    setLoading(true);
    setError(null);
    const timer = window.setTimeout(() => { void requestPage(url, null, 0); }, 250);
    return () => window.clearTimeout(timer);
  }, [requestPage, url]);

  useEffect(() => () => { ++generation.current; controller.current?.abort(); }, []);

  function next() { if (!loading && loadedUrl.current === url && page.nextCursor) {void requestPage(url, page.nextCursor, index + 1);} }
  function previous() { if (!loading && loadedUrl.current === url && index > 0) {void requestPage(url, cursors.current[index - 1], index - 1);} }
  function retry() {
    if (loading || controller.current) {return;}
    const failed = failedRequest.current;
    if (failed?.url === url) {void requestPage(failed.url, failed.cursor, failed.index); return;}
    void requestPage(url, loadedUrl.current === url ? cursors.current[index] ?? null : null, loadedUrl.current === url ? index : 0);
  }
  return { page, index, loading, error, next, previous, retry };
}
