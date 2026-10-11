"use client";
import { useCallback, useEffect, useState } from "react";
import type { RuntimeStateV1 } from "./runtime/contract";

export function usePlayerHud(state: RuntimeStateV1, pinned: boolean, desktopPointer = false) {
  const [revealed, setRevealed] = useState(true);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [revision, setRevision] = useState(0);
  const held = (state !== "RUNNING" && !(desktopPointer && state === "PAUSED")) || pinned || hovered || focused;
  useEffect(() => {
    if (held) {
      return;
    }
    const timer = window.setTimeout(() => setRevealed(false), 2000);
    return () => window.clearTimeout(timer);
  }, [held, revision]);
  const show = useCallback(() => {
    setRevealed(true);
    setRevision((value) => value + 1);
  }, []);
  const onHover = useCallback((value: boolean) => {
    setHovered(value);
    if (desktopPointer && !value) { setRevealed(false); }
  }, [desktopPointer]);
  return {
    visible: held || revealed,
    show,
    onHover,
    onFocus: setFocused,
  };
}
