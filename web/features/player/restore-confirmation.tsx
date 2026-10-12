"use client";
import { createContext, useCallback, useEffect, useRef, useState } from "react";

export const restoreConfirmationTitle = "尝试加载旧存档？";
export const restoreConfirmationDescription = "当前核心与创建此存档时的版本不一致，加载可能失败，或在游玩过程中出现异常。你仍可尝试加载。原存档会保留，后续保存的进度将另存为新存档。";
export const RestoreConfirmationContext = createContext<(() => Promise<boolean>) | null>(null);

export function useRestoreConfirmation() {
  const [open, setOpen] = useState(false);
  const pending = useRef<((confirmed: boolean) => void) | null>(null);
  const request = useCallback(() => new Promise<boolean>((resolve) => {
    pending.current?.(false);
    pending.current = resolve;
    setOpen(true);
  }), []);
  const settle = useCallback((confirmed: boolean) => {
    pending.current?.(confirmed);
    pending.current = null;
    setOpen(false);
  }, []);
  useEffect(() => () => { pending.current?.(false); }, []);
  return { open, request, settle };
}
