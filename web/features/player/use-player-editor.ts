"use client";

import { useEffect, useRef, useState, type RefObject } from "react";
import type { PlayerRuntimeV1, RuntimeStateV1 } from "./runtime/contract";

export function usePlayerEditor(runtime: RefObject<PlayerRuntimeV1 | null>, state: RuntimeStateV1, onError: (message: string) => void) {
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const wasOpen = useRef(false);
  const opening = useRef(false);
  useEffect(() => {
    if (wasOpen.current && !open) { document.getElementById("player-more-button")?.focus(); }
    wasOpen.current = open;
  }, [open]);
  const editor = state === "RUNNING" || state === "PAUSED" ? runtime.current?.getGameEditor?.() ?? null : null;
  async function show() {
    const instance = runtime.current;
    if (!instance || !editor || opening.current) { return; }
    opening.current = true;
    setPending(true);
    try {
      if (instance.getState() === "RUNNING" && instance.getCapabilities().pause) { await instance.pause(); }
      setOpen(true);
    } catch (error) {
      onError(error instanceof Error ? error.message : "无法打开游戏修改。");
    } finally { opening.current = false; setPending(false); }
  }
  return { editor, open, pending, show, close: () => setOpen(false) };
}
