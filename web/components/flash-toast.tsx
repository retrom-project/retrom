"use client";

import Link from "next/link";
import { useEffect, useEffectEvent, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useOverlayTarget } from "./overlay-target";

type ToastLink = { href: string; label: string };
type ToastAction = { label: string; onPress: () => void | Promise<void>; focusIfOrphaned?: boolean };
export type ToastMessage = {
  message: string;
  tone: "good" | "warn" | "bad";
  action?: ToastLink | ToastAction;
  durationMs?: number;
};

export type FlashToastMessage = Omit<ToastMessage, "action"> & { action?: ToastLink };

const flashKey = "retrom:flash-toast";

export function queueFlashToast(toast: FlashToastMessage) {
  sessionStorage.setItem(flashKey, JSON.stringify(toast));
}

export function Toast({ toast, onDismiss }: { toast: ToastMessage | null; onDismiss: () => void }) {
  const target = useOverlayTarget();
  const [pending, setPending] = useState<ToastMessage | null>(null);
  const running = useRef<ToastMessage | null>(null);
  const button = useRef<HTMLButtonElement>(null);
  const busy = Boolean(toast && pending === toast);
  const dismiss = useEffectEvent(onDismiss);
  useEffect(() => {
    if (!toast || busy) {return;}
    const timer = window.setTimeout(dismiss, toast.durationMs ?? 3_000);
    return () => window.clearTimeout(timer);
  }, [toast, busy]);
  useEffect(() => {
    if (!toast?.action || !("onPress" in toast.action) || !toast.action.focusIfOrphaned) {return;}
    const frame = requestAnimationFrame(() => {
      if (document.activeElement === document.body) {button.current?.focus();}
    });
    return () => cancelAnimationFrame(frame);
  }, [toast]);

  async function runAction() {
    if (!toast?.action || !("onPress" in toast.action) || running.current === toast) {return;}
    running.current = toast;
    setPending(toast);
    try { await toast.action.onPress(); }
    finally {
      if (running.current === toast) {running.current = null;}
      setPending((current) => current === toast ? null : current);
    }
  }

  if (!toast || !target) {return null;}
  return createPortal(<div className={`app-toast ${toast.tone}`} role={toast.tone === "bad" ? "alert" : "status"} aria-live={toast.tone === "bad" ? "assertive" : "polite"}>
    <span>{toast.message}</span>
    {toast.action ? "href" in toast.action
      ? <Link href={toast.action.href}>{toast.action.label}</Link>
      : <button ref={button} className="button secondary" type="button" disabled={busy} onClick={() => void runAction()}>{toast.action.label}</button>
      : null}
  </div>, target);
}

export function takeFlashToast(): ToastMessage | null {
  const raw = sessionStorage.getItem(flashKey);
  if (!raw) {return null;}
  sessionStorage.removeItem(flashKey);
  try {
    const parsed = JSON.parse(raw) as Partial<ToastMessage>;
    if (typeof parsed.message === "string" && (parsed.tone === "good" || parsed.tone === "warn" || parsed.tone === "bad")) {
      return { message: parsed.message, tone: parsed.tone };
    }
  } catch {
    // Malformed local notification state can be discarded.
  }
  return null;
}
