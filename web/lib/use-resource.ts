"use client";
import { useCallback, useEffect, useState } from "react";
export type Resource<T> = {
  data: T | null;
  error: string;
  loading: boolean;
  reload: () => void;
};
export function useResource<T>(loader: () => Promise<T>): Resource<T> {
  const [state, setState] = useState<{
    data: T | null;
    error: string;
    loading: boolean;
    source: () => Promise<T>;
  }>({ data: null, error: "", loading: true, source: loader });
  const [revision, setRevision] = useState(0);
  const reload = useCallback(() => setRevision((value) => value + 1), []);
  useEffect(() => {
    let active = true;
    void loader()
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
    };
  }, [loader, revision]);
  return state.source === loader
    ? { ...state, reload }
    : { data: null, error: "", loading: true, reload };
}
