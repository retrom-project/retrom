"use client";

import { useEffect, useEffectEvent, type RefObject } from "react";

export function usePlayerMenuDismiss(
  mount: RefObject<HTMLElement | null>,
  open: boolean,
  onDismiss: () => void,
) {
  const dismiss = useEffectEvent(onDismiss);
  useEffect(() => {
    const surface = mount.current;
    if (!open || !surface) { return; }
    let pendingFrame: number | null = null;
    function pointerDown() { dismiss(); }
    function windowBlurred() {
      if (pendingFrame !== null) { window.cancelAnimationFrame(pendingFrame); }
      // The Host owns the iframe element. Reading its focus does not inspect
      // the game document and works for both same-origin and isolated frames.
      pendingFrame = window.requestAnimationFrame(() => {
        pendingFrame = null;
        const active = document.activeElement;
        if (document.hasFocus() && active instanceof HTMLIFrameElement && surface?.contains(active)) {
          dismiss();
        }
      });
    }
    surface.addEventListener("pointerdown", pointerDown, true);
    window.addEventListener("blur", windowBlurred);
    return () => {
      surface.removeEventListener("pointerdown", pointerDown, true);
      window.removeEventListener("blur", windowBlurred);
      if (pendingFrame !== null) { window.cancelAnimationFrame(pendingFrame); }
    };
  }, [mount, open]);
}
