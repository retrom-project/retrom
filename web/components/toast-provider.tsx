"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { Toast, takeFlashToast, type ToastMessage } from "./flash-toast";

type ToastContextValue = { notify: (toast: ToastMessage) => void; clear: () => void };
const ToastContext = createContext<ToastContextValue | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<ToastMessage | null>(null);
  const notify = useCallback((message: ToastMessage) => setToast({ ...message }), []);
  const clear = useCallback(() => setToast(null), []);
  const value = useMemo(() => ({ notify, clear }), [notify, clear]);
  return <ToastContext.Provider value={value}>
    {children}
    <Toast toast={toast} onDismiss={clear} />
  </ToastContext.Provider>;
}

export function useToast() {
  const context = useContext(ToastContext);
  if (!context) {throw new Error("useToast requires ToastProvider");}
  return context;
}

// The destination page consumes persisted messages through the same notification owner.
export function FlashToast() {
  const { notify } = useToast();
  useEffect(() => {
    const toast = takeFlashToast();
    if (toast) {notify(toast);}
  }, [notify]);
  return null;
}
