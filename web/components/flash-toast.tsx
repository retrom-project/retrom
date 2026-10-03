"use client";

import Link from "next/link";
import { useEffect, useEffectEvent, useState } from "react";
import { createPortal } from "react-dom";
import { useOverlayTarget } from "./overlay-target";

export type ToastMessage = { message: string; tone: "good" | "warn" | "bad"; action?: { href: string; label: string } };

const flashKey = "retrom:flash-toast";

export function queueFlashToast(toast: ToastMessage) {
  sessionStorage.setItem(flashKey, JSON.stringify(toast));
}

export function Toast({ toast, onDismiss }: { toast: ToastMessage | null; onDismiss: () => void }) {
  const target = useOverlayTarget();
  const dismiss = useEffectEvent(onDismiss);
  useEffect(() => {
    if (!toast) {return;}
    const timer = window.setTimeout(dismiss, 3_000);
    return () => window.clearTimeout(timer);
  }, [toast]);

  if (!toast || !target) {return null;}
  return createPortal(<div className={`app-toast ${toast.tone}`} role={toast.tone === "bad" ? "alert" : "status"} aria-live={toast.tone === "bad" ? "assertive" : "polite"}>
    <span>{toast.message}</span>
    {toast.action ? <Link href={toast.action.href}>{toast.action.label}</Link> : null}
  </div>, target);
}

export function FlashToast() {
  const [toast, setToast] = useState<ToastMessage | null>(null);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      const raw = sessionStorage.getItem(flashKey);
      if (!raw) {return;}
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
