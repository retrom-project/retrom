"use client";

import { useEffect, useState } from "react";

export type ToastMessage = { message: string; tone: "good" | "warn" | "bad" };

const flashKey = "retrom:flash-toast";

export function queueFlashToast(toast: ToastMessage) {
  sessionStorage.setItem(flashKey, JSON.stringify(toast));
}

export function Toast({ toast, onDismiss }: { toast: ToastMessage | null; onDismiss: () => void }) {
  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(onDismiss, 2_000);
    return () => window.clearTimeout(timer);
  }, [onDismiss, toast]);

  if (!toast) return null;
  return <div className={`app-toast ${toast.tone}`} role={toast.tone === "bad" ? "alert" : "status"} aria-live="polite">
    <span>{toast.message}</span>
    <button type="button" aria-label="关闭通知" onClick={onDismiss}>×</button>
  </div>;
}

export function FlashToast() {
  const [toast, setToast] = useState<ToastMessage | null>(null);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      const raw = sessionStorage.getItem(flashKey);
      if (!raw) return;
      sessionStorage.removeItem(flashKey);
      try {
        const parsed = JSON.parse(raw) as Partial<ToastMessage>;
        if (typeof parsed.message === "string" && (parsed.tone === "good" || parsed.tone === "warn" || parsed.tone === "bad")) {
          setToast({ message: parsed.message, tone: parsed.tone });
        }
      } catch {
        // A malformed, local-only flash value is safe to discard.
      }
    }, 0);
    return () => window.clearTimeout(timer);
  }, []);
  return <Toast toast={toast} onDismiss={() => setToast(null)} />;
}
