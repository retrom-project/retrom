"use client";

import { useEffect, useEffectEvent, useRef, useState } from "react";
import { useToast } from "@/components/toast-provider";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";

type Replacement = Schema<"ContentReplacement">;
export const replacementActive = (value: Replacement | null) =>
  value !== null && ["queued", "running", "canceling"].includes(value.status);

export function useContentReplacement(gameId: string, version: number, onChange: () => void) {
  const { notify } = useToast();
  const [value, setValue] = useState<Replacement | null>(null);
  const [checked, setChecked] = useState(false);
  const [busy, setBusy] = useState(false);
  const [readError, setReadError] = useState("");
  const [expired, setExpired] = useState(false);
  const [retry, setRetry] = useState(0);
  const generation = useRef(0);
  const observed = useRef<string | null>(null);
  const synchronized = useRef<string | null>(null);
  const operation = value?.gameId === gameId ? value : null;
  const operationId = operation?.id;
  const active = replacementActive(operation) && !expired;
  const completed = useEffectEvent(() => {
    notify({ tone: "good", message: "游戏内容已替换" });
    onChange();
  });
  const refreshDetail = useEffectEvent(onChange);

  useEffect(() => {
    if (operation?.status !== "completed" || synchronized.current === operation.id) { return; }
    if (observed.current === operation.id) {
      observed.current = null;
      synchronized.current = operation.id;
      completed();
    } else if (operation.result && operation.result.version > version) {
      synchronized.current = operation.id;
      refreshDetail();
    }
  }, [operation?.id, operation?.status, operation?.result, version]);

  useEffect(() => {
    if (operationId && !active) { return; }
    const controller = new AbortController();
    const current = ++generation.current;
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function load() {
      try {
        const response = operationId
          ? await api.GET("/api/v1/admin/games/{gameId}/content-replacement/{operationId}", {
            params: { path: { gameId, operationId } }, signal: controller.signal,
          })
          : await api.GET("/api/v1/admin/games/{gameId}/content-replacement", {
            params: { path: { gameId } }, signal: controller.signal,
          });
        if (controller.signal.aborted || current !== generation.current) { return; }
        if (response.response.status === 404) {
          setChecked(true);
          if (operationId) {
            setExpired(true);
            setReadError("替换进度已过期或被后续操作替代，请刷新游戏资料核对结果。");
          } else { setReadError(""); }
          return;
        }
        const next = result(response);
        if (replacementActive(next)) { observed.current = next.id; }
        setValue(next);
        setChecked(true);
        setReadError("");
        if (replacementActive(next)) { timer = setTimeout(() => void load(), 2000); }
      } catch (failure) {
        if (!controller.signal.aborted && current === generation.current) {
          setReadError(failure instanceof Error ? failure.message : "无法读取替换进度，请重试。");
        }
      }
    }
    if (operationId) { timer = setTimeout(() => void load(), 2000); }
    else { void load(); }
    return () => { controller.abort(); clearTimeout(timer); };
  }, [gameId, operationId, active, retry]);

  async function start(path: string, version: number) {
    setBusy(true);
    generation.current++;
    try {
      const accepted = result(await api.POST("/api/v1/admin/games/{gameId}/content-replacement", {
        params: { path: { gameId } }, body: { path, version },
      }));
      observed.current = accepted.id;
      setExpired(false);
      setReadError("");
      setValue(accepted);
      return true;
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "无法确认替换提交结果，请刷新进度核对。" });
      setValue(null);
      setChecked(false);
      setRetry((count) => count + 1);
      return false;
    } finally { setBusy(false); }
  }

  async function cancel() {
    if (!operation?.canCancel) { return; }
    setBusy(true);
    generation.current++;
    try {
      const next = result(await api.POST("/api/v1/admin/games/{gameId}/content-replacement/{operationId}/cancel", {
        params: { path: { gameId, operationId: operation.id } },
      }));
      setValue(next);
      setReadError("");
    } catch (failure) {
      setReadError(failure instanceof Error ? failure.message : "无法确认取消结果，请重试读取进度。");
    } finally { setBusy(false); setRetry((count) => count + 1); }
  }

  return { operation, active, checked, busy, readError, expired, start, cancel,
    refresh: () => { setExpired(false); setRetry((count) => count + 1); } };
}
