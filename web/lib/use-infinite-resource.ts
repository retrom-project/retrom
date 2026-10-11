"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import type { Resource } from "./use-resource";

export type PageRequest = { offset: number; limit: number; signal: AbortSignal };
type Page = { items: unknown[]; total: number; offset: number; limit: number };
type Loader<P> = (request: PageRequest) => Promise<P>;
type State<P> = { source: Loader<P>; data: P | null; error: string; loading: boolean; loadingMore: boolean; moreError: string; hasMore: boolean };
export type InfiniteResource<P> = Resource<P> & { loadingMore: boolean; moreError: string; hasMore: boolean; loadMore: () => void };
const PAGE_SIZE = 24;

export function useInfiniteResource<P extends Page>(
  loader: Loader<P>,
  itemKey: (item: P["items"][number]) => string,
  initialCount = PAGE_SIZE,
): InfiniteResource<P> {
  const [state, setState] = useState<State<P>>(() => initialState(loader));
  const actions = useRef({ more: () => {}, reload: () => {} });
  const loadMore = useCallback(() => actions.current.more(), []);
  const reload = useCallback(() => actions.current.reload(), []);
  useEffect(() => {
    let active = true;
    let current = initialState(loader);
    let nextOffset = 0;
    let request: AbortController | null = null;
    let retryRefresh = false;
    function publish(changes: Partial<State<P>>) {
      current = { ...current, ...changes };
      if (active) { setState(current); }
    }
    async function read(refresh: boolean) {
      if (!active || (!refresh && (request || !current.hasMore))) { return; }
      request?.abort();
      const controller = new AbortController();
      request = controller;
      // A mutation refreshes only the range already loaded, in bounded pages.
      // Keep the visible cards until the replacement succeeds.
      const target = refresh ? Math.max(PAGE_SIZE, initialCount, nextOffset) : nextOffset + PAGE_SIZE;
      retryRefresh = refresh;
      publish({ error: "", moreError: "", loadingMore: !!current.data, loading: !current.data });
      try {
        const { data, offset, hasMore } = await readPages(loader, itemKey, controller.signal, {
          offset: refresh ? 0 : nextOffset, data: refresh ? null : current.data, target, refresh,
        });
        if (controller.signal.aborted) { return; }
        nextOffset = offset;
        publish({ data, loading: false, loadingMore: false, hasMore });
      } catch (failure) {
        if (controller.signal.aborted) { return; }
        const message = failure instanceof Error ? failure.message : "加载失败，请重试。";
        publish(current.data
          ? { loadingMore: false, moreError: message }
          : { loading: false, error: message });
      } finally {
        if (request === controller) { request = null; }
      }
    }
    actions.current = {
      more: () => void read(current.moreError ? retryRefresh : false),
      reload: () => void read(true),
    };
    void read(true);
    return () => { active = false; request?.abort(); };
  }, [loader, itemKey, initialCount]);
  return { ...(state.source === loader ? state : initialState(loader)), loadMore, reload };
}

async function readPages<P extends Page>(loader: Loader<P>, key: (item: P["items"][number]) => string,
  signal: AbortSignal, range: { data: P | null; offset: number; target: number; refresh: boolean }) {
  let { data, offset } = range;
  let hasMore: boolean;
  do {
    const page = await loader({ offset, limit: PAGE_SIZE, signal });
    signal.throwIfAborted();
    data = mergePage(data, page, key);
    offset = page.offset + page.items.length;
    hasMore = page.items.length > 0 && offset < page.total;
  } while (range.refresh && hasMore && offset < range.target);
  return { data, offset, hasMore };
}

function initialState<P>(source: Loader<P>): State<P> {
  return { source, data: null, error: "", loading: true, loadingMore: false, moreError: "", hasMore: false };
}

function mergePage<P extends Page>(previous: P | null, page: P, key: (item: P["items"][number]) => string): P {
  const items = new Map<string, P["items"][number]>();
  for (const item of previous?.items ?? []) { items.set(key(item), item); }
  for (const item of page.items) { items.set(key(item), item); }
  return { ...page, items: [...items.values()] };
}
