"use client";
import { useCallback, useEffect, useState } from "react";
import type { Resource } from "@/lib/use-resource";

export function isScanRunning(scan: { status: string } | null) {
  return scan?.status === "pending" || scan?.status === "running";
}

// Only an active, visible scan needs periodic reads. A response schedules the
// next read, so a slow server cannot accumulate overlapping requests.
export function useScanPolling<T>(
  loader: (signal: AbortSignal) => Promise<T>,
  isRunning: (data: T) => boolean,
  revision = 0,
): Resource<T> {
  const [state, setState] = useState({ data: null as T | null, error: "", loading: true, source: loader });
  const [refresh, setRefresh] = useState(0);
  const reload = useCallback(() => setRefresh((value) => value + 1), []);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    let reading = false;
    async function read() {
      clearTimeout(timer);
      if (reading || controller.signal.aborted || document.visibilityState === "hidden") { return; }
      reading = true;
      try {
        const data = await loader(controller.signal);
        if (controller.signal.aborted) { return; }
        setState({ data, error: "", loading: false, source: loader });
        if (isRunning(data) && !document.hidden) {
          timer = setTimeout(() => void read(), 3000);
        }
      } catch (failure) {
        if (!controller.signal.aborted) {
          setState({ data: null, error: failure instanceof Error ? failure.message : "读取扫描进度失败。", loading: false, source: loader });
        }
      } finally { reading = false; }
    }
    function visibilityChanged() { clearTimeout(timer); void read(); }
    document.addEventListener("visibilitychange", visibilityChanged);
    void read();
    return () => {
      controller.abort();
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", visibilityChanged);
    };
  }, [loader, isRunning, revision, refresh]);
  return state.source === loader ? { ...state, reload } : { data: null, error: "", loading: true, reload };
}
