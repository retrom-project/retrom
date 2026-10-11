"use client";
import { useCallback, useEffect, useState } from "react";
export type Resource<T> = {
  data: T | null;
  error: string;
  loading: boolean;
  reload: () => void;
};
export function useResource<T>(loader: (signal?: AbortSignal) => Promise<T>): Resource<T> {
  const [state, setState] = useState<{
    data: T | null;
    error: string;
    loading: boolean;
    source: (signal?: AbortSignal) => Promise<T>;
  }>({ data: null, error: "", loading: true, source: loader });
  const [revision, setRevision] = useState(0);
  const reload = useCallback(() => setRevision((value) => value + 1), []);
  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    void loader(controller.signal)
      .then((data) => {
        if (active) {
          setState({ data, error: "", loading: false, source: loader });
        }
      })
      .catch((error: unknown) => {
        if (active) {
          setState({
            data: null,
            source: loader,
            error: error instanceof Error ? error.message : "加载失败。",
            loading: false,
          });
        }
      });
    return () => {
      active = false;
      controller.abort();
    };
  }, [loader, revision]);
  return state.source === loader
    ? { ...state, reload }
    : { data: null, error: "", loading: true, reload };
}
