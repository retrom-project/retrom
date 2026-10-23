"use client";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { InfiniteResource, PageRequest } from "@/lib/use-infinite-resource";
import { entryId, type ImmersivePage } from "./immersive-data";

const PAGE_SIZE = 24;
type Loader = (request: PageRequest) => Promise<ImmersivePage>;
type WindowState = {
  source: Loader; data: ImmersivePage | null; error: string; loading: boolean;
  loadingMore: boolean; moreError: string; loadingBefore: boolean;
};

/**
 * A contiguous window of a long list that can start anywhere. Jumping to a far letter opens a new
 * window at that offset instead of reading every page before it; moving up loads earlier pages.
 */
export type EntryWindow = InfiniteResource<ImmersivePage> & {
  /** List offset of the first loaded item. */
  base: number;
  hasBefore: boolean;
  loadingBefore: boolean;
  /** Prepends the previous page and resolves with the number of items added. */
  loadBefore: () => Promise<number>;
  /** Resolves with the window index of a list offset, opening a new window when it is not loaded. */
  moveTo: (offset: number) => Promise<number | null>;
};

export function useEntryWindow(loader: Loader, startOffset: number): EntryWindow {
  const [state, setState] = useState<WindowState>(() => initialState(loader));
  const current = useRef(state);
  const request = useRef<AbortController | null>(null);
  // The start offset only seeds a new list; later moves keep their own window.
  const start = useRef(startOffset);
  useLayoutEffect(() => { start.current = startOffset; }, [startOffset]);
  const publish = useCallback((changes: Partial<WindowState>) => {
    current.current = { ...current.current, ...changes };
    setState(current.current);
  }, []);
  const read = useCallback(async (offset: number, limit: number) => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller;
    try {
      const page = await loader({ offset, limit, signal: controller.signal });
      controller.signal.throwIfAborted();
      return page;
    } finally {
      if (request.current === controller) { request.current = null; }
    }
  }, [loader]);
  const open = useCallback(async (offset: number): Promise<boolean> => {
    publish({ source: loader, loading: !current.current.data, loadingMore: !!current.current.data, error: "", moreError: "" });
    try {
      let page = await read(offset, PAGE_SIZE);
      if (!page.items.length && offset > 0 && page.total > 0) { page = await read(Math.max(0, page.total - PAGE_SIZE), PAGE_SIZE); }
      publish({ data: page, loading: false, loadingMore: false });
      return true;
    } catch (failure) {
      if (failure instanceof DOMException && failure.name === "AbortError") { return false; }
      const message = failure instanceof Error ? failure.message : "加载失败，请重试。";
      publish(current.current.data ? { loadingMore: false, moreError: message } : { loading: false, error: message });
      return false;
    }
  }, [loader, publish, read]);

  useEffect(() => {
    current.current = initialState(loader);
    setState(current.current);
    void open(Math.max(0, start.current));
    return () => request.current?.abort();
  }, [loader, open]);

  const loadMore = useCallback(() => {
    const data = current.current.data;
    if (!data || request.current || data.offset + data.items.length >= data.total) { return; }
    publish({ loadingMore: true, moreError: "" });
    void read(data.offset + data.items.length, PAGE_SIZE).then((page) => {
      const latest = current.current.data ?? data;
      publish({ data: { ...latest, total: page.total, items: merge(latest.items, page.items) }, loadingMore: false });
    }, (failure: unknown) => {
      if (failure instanceof DOMException && failure.name === "AbortError") { return; }
      publish({ loadingMore: false, moreError: failure instanceof Error ? failure.message : "加载失败，请重试。" });
    });
  }, [publish, read]);

  const loadBefore = useCallback(async () => {
    const data = current.current.data;
    if (!data || data.offset === 0 || request.current) { return 0; }
    const start = Math.max(0, data.offset - PAGE_SIZE);
    publish({ loadingBefore: true, moreError: "" });
    try {
      const page = await read(start, data.offset - start);
      const latest = current.current.data ?? data;
      const items = merge(page.items, latest.items);
      publish({ data: { ...latest, offset: start, total: page.total, items }, loadingBefore: false });
      return items.length - latest.items.length;
    } catch (failure) {
      publish({ loadingBefore: false, moreError: failure instanceof DOMException && failure.name === "AbortError" ? "" : failure instanceof Error ? failure.message : "加载失败，请重试。" });
      return 0;
    }
  }, [publish, read]);

  const moveTo = useCallback(async (offset: number) => {
    const data = current.current.data;
    const end = data ? data.offset + data.items.length : 0;
    // A loaded target keeps the window only when half a page follows it, so it can be shown at the top.
    const followed = !!data && (end - offset >= PAGE_SIZE / 2 || end >= data.total);
    if (data && offset >= data.offset && offset < end && followed) { return offset - data.offset; }
    return await open(offset) ? Math.max(0, offset - (current.current.data?.offset ?? offset)) : null;
  }, [open]);

  // A favorite change refreshes the visible range in bounded pages, keeping it until replaced.
  const reload = useCallback(() => {
    const previous = current.current.data;
    const offset = previous?.offset ?? Math.max(0, start.current);
    const wanted = Math.max(PAGE_SIZE, previous?.items.length ?? 0);
    publish({ loading: !previous, loadingMore: !!previous, error: "", moreError: "" });
    void (async () => {
      try {
        let page = await read(offset, PAGE_SIZE);
        while (page.items.length < wanted && page.offset + page.items.length < page.total) {
          const next = await read(page.offset + page.items.length, PAGE_SIZE);
          if (!next.items.length) { break; }
          page = { ...next, offset: page.offset, items: merge(page.items, next.items) };
        }
        publish({ data: page, loading: false, loadingMore: false });
      } catch (failure) {
        if (failure instanceof DOMException && failure.name === "AbortError") { return; }
        const message = failure instanceof Error ? failure.message : "加载失败，请重试。";
        publish(current.current.data ? { loadingMore: false, moreError: message } : { loading: false, error: message });
      }
    })();
  }, [publish, read]);

  const visible = state.source === loader ? state : initialState(loader);
  const data = visible.data;
  return {
    ...visible, data, reload, loadMore, loadBefore, moveTo,
    base: data?.offset ?? 0,
    hasBefore: (data?.offset ?? 0) > 0,
    hasMore: !!data && data.offset + data.items.length < data.total,
  };
}

function initialState(source: Loader): WindowState {
  return { source, data: null, error: "", loading: true, loadingMore: false, moreError: "", loadingBefore: false };
}

function merge(first: ImmersivePage["items"], second: ImmersivePage["items"]) {
  const items = new Map<string, ImmersivePage["items"][number]>();
  for (const item of [...first, ...second]) { items.set(entryId(item), item); }
  return [...items.values()];
}
