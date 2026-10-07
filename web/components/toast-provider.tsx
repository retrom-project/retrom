"use client";

import { createContext, useCallback, useContext, useEffect, useEffectEvent, useMemo, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { AppIcon } from "./app-icon";
import { useOverlayTarget } from "./overlay-target";

export type ToastMessage = {
  message: string;
  tone: "good" | "warn" | "bad";
  durationMs?: number;
};
type ToastContextValue = { notify: (message: ToastMessage) => void; clear: () => void };
const ToastContext = createContext<ToastContextValue | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<ToastMessage | null>(null);
  const origin = useRef<HTMLElement | null>(null);
  const notify = useCallback((message: ToastMessage) => {
    origin.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setToast({ ...message });
  }, []);
  const clear = useCallback(() => setToast(null), []);
  const value = useMemo(() => ({ notify, clear }), [notify, clear]);
  function close() {
    clear();
    if (origin.current?.isConnected) { origin.current.focus(); }
  }
  return <ToastContext value={value}>
    {children}
    <ToastNotification toast={toast} onDismiss={clear} onClose={close} />
  </ToastContext>;
}

function ToastNotification({ toast, onDismiss, onClose }: {
  toast: ToastMessage | null;
  onDismiss: () => void;
  onClose: () => void;
}) {
  const target = useOverlayTarget();
  const dismiss = useEffectEvent(onDismiss);
  useEffect(() => {
    if (!toast) { return; }
    const timer = window.setTimeout(dismiss, toast.durationMs ?? 3_000);
    return () => window.clearTimeout(timer);
  }, [toast]);
  if (!toast || !target) { return null; }
  return createPortal(
    <div className={`app-toast ${toast.tone}`} role={toast.tone === "bad" ? "alert" : "status"} aria-live={toast.tone === "bad" ? "assertive" : "polite"} aria-atomic="true">
      <span>{toast.message}</span>
      <button className="button ghost icon-only app-toast-close" type="button" aria-label="关闭通知" onClick={onClose}><AppIcon name="x" /></button>
    </div>,
    target,
  );
}

export function useToast() {
  const context = useContext(ToastContext);
  if (!context) { throw new Error("useToast requires ToastProvider"); }
  return context;
}
