"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { formatBytes } from "@/lib/format-bytes";
import type { ContentCacheSummaryV1 } from "@/features/player/runtime/contract";
import { clearManagedContentCache, loadContentCacheManager, readSiteUsage, type ContentCacheManager } from "./browser-storage";

type StorageState = { usage: number | null; cache: ContentCacheSummaryV1 | null; usageError: string; cacheError: string };
const emptyState: StorageState = { usage: null, cache: null, usageError: "", cacheError: "" };

export function useBrowserStorage() {
  const [state, setState] = useState(emptyState);
  const [busy, setBusy] = useState<"refresh" | "clear" | null>("refresh");
  const [message, setMessage] = useState("");
  const task = useRef<AbortController | null>(null);
  const manager = useRef<ContentCacheManager | null>(null);
  const update = useCallback(async (signal: AbortSignal) => {
    const [usage, cache] = await Promise.allSettled([
      readSiteUsage(),
      (async () => {
        manager.current ??= await loadContentCacheManager(signal);
        return manager.current.inspectContentCache({ signal });
      })(),
    ]);
    if (signal.aborted) { return; }
    setState({
      usage: usage.status === "fulfilled" ? usage.value : null,
      usageError: usage.status === "rejected" ? "当前浏览器暂时无法提供站点占用估算。" : "",
      cache: cache.status === "fulfilled" ? cache.value : null,
      cacheError: cache.status === "rejected" ? "暂时无法读取资源缓存，请检查连接或浏览器存储权限后刷新。" : "",
    });
  }, []);
  const refresh = useCallback(async () => {
    if (task.current) { return; }
    const controller = new AbortController(); task.current = controller;
    setBusy("refresh");
    try { await update(controller.signal); }
    finally {
      if (task.current === controller) { task.current = null; setBusy(null); }
    }
  }, [update]);
  useEffect(() => {
    void refresh();
    return () => { task.current?.abort(); task.current = null; manager.current = null; };
  }, [refresh]);

  async function clear() {
    if (task.current || !manager.current) { return; }
    const controller = new AbortController(); task.current = controller;
    setBusy("clear"); setMessage("正在检查并清理闲置资源缓存…");
    try {
      const result = await clearManagedContentCache(manager.current, controller.signal, progress => {
        if (!controller.signal.aborted) { setMessage(`正在清理…已删除 ${progress.removedObjects} 项资源缓存。`); }
      });
      if (task.current !== controller) { return; }
      const skipped = result.skippedObjects ? `另有 ${result.skippedObjects} 项正在使用或暂时无法清理。` : "";
      setMessage(`已清理 ${result.removedObjects} 项资源缓存，有效数据约 ${formatBytes(result.reclaimedBytes)}。${skipped}`);
    } catch {
      if (task.current !== controller) { return; }
      setMessage(controller.signal.aborted
        ? "已停止清理，已删除的缓存不会恢复；剩余数据已重新统计。"
        : "清理未完成，部分缓存可能已移除。请查看剩余数据后重试。");
    } finally {
      if (task.current === controller) {
        const reader = new AbortController(); task.current = reader;
        setBusy("refresh");
        await update(reader.signal);
        if (task.current === reader) { task.current = null; setBusy(null); }
      }
    }
  }
  return { ...state, busy, message, refresh, clear, stop: () => task.current?.abort() };
}
