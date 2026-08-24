"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { requestImmersiveFullscreen } from "./immersive-fullscreen";

const RESTORE_VISIBLE_MS = 4_000;
const TOP_EDGE_REVEAL_PX = 72;

export function useImmersiveFullscreen() {
  const [active, setActive] = useState(false);
  const [supported, setSupported] = useState(false);
  const [restoreVisible, setRestoreVisible] = useState(false);
  const hideTimerRef = useRef<number | null>(null);

  const hideLater = useCallback(() => {
    if (hideTimerRef.current !== null) {window.clearTimeout(hideTimerRef.current);}
    setRestoreVisible(true);
    hideTimerRef.current = window.setTimeout(() => setRestoreVisible(false), RESTORE_VISIBLE_MS);
  }, []);

  const enterFullscreen = useCallback(() => {
    void requestImmersiveFullscreen().then((entered) => {
      if (!entered) {hideLater();}
    });
  }, [hideLater]);

  useEffect(() => {
    const available = typeof document.documentElement.requestFullscreen === "function";
    const update = () => {
      const next = Boolean(document.fullscreenElement);
      setActive(next);
      if (next) {setRestoreVisible(false);}
      else if (available) {hideLater();}
    };
    const revealAtTop = (event: PointerEvent) => {
      if (event.clientY <= TOP_EDGE_REVEAL_PX && !document.fullscreenElement) {hideLater();}
    };
    const initializeTimer = window.setTimeout(() => {
      setSupported(available);
      update();
    }, 0);
    document.addEventListener("fullscreenchange", update);
    window.addEventListener("pointermove", revealAtTop, { passive: true });
    return () => {
      document.removeEventListener("fullscreenchange", update);
      window.removeEventListener("pointermove", revealAtTop);
      window.clearTimeout(initializeTimer);
      if (hideTimerRef.current !== null) {window.clearTimeout(hideTimerRef.current);}
    };
  }, [hideLater]);

  return { active, enterFullscreen, restoreVisible, supported };
}
