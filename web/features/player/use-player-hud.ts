"use client";
import { useCallback, useEffect, useState } from "react";
import type { RuntimeStateV1 } from "./runtime/contract";

export function usePlayerHud(state: RuntimeStateV1, pinned: boolean, desktopPointer = false) {
  const displayState = useCheckpointDisplayState(state);
  const [revealed, setRevealed] = useState(true);
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [revision, setRevision] = useState(0);
  const [dismissed, setDismissed] = useState(false);
  const [openedByTouch, setOpenedByTouch] = useState(false);
  const held = (displayState !== "RUNNING" && !(desktopPointer && displayState === "PAUSED")) || pinned || hovered || focused || (!desktopPointer && openedByTouch);
  useEffect(() => {
    if (held) {
      return;
    }
    const timer = window.setTimeout(() => setRevealed(false), 2000);
    return () => window.clearTimeout(timer);
  }, [held, revision]);
  const show = useCallback(() => {
    setDismissed(false);
    setOpenedByTouch(true);
    setRevealed(true);
    setRevision((value) => value + 1);
  }, []);
  const onHover = useCallback((value: boolean) => {
    if (!desktopPointer) { return; }
    if (value) { setDismissed(false); }
    setHovered(value);
    if (desktopPointer && !value) { setRevealed(false); }
  }, [desktopPointer]);
  const hide = useCallback(() => {
    setRevealed(false);
    setHovered(false);
    setFocused(false);
    setOpenedByTouch(false);
    setDismissed(true);
  }, []);
  const onFocus = useCallback((value: boolean) => {
    setFocused(value);
    if (value) { setDismissed(false); }
  }, []);
  return {
    visible: pinned || (displayState !== "RUNNING" && displayState !== "PAUSED") || (!dismissed && (held || revealed)),
    show,
    hide,
    onHover,
    onFocus,
  };
}

function useCheckpointDisplayState(state: RuntimeStateV1) {
  // Exporting a checkpoint is transient work, not a request to reveal controls.
  const [previousState, setPreviousState] = useState(state);
  if (state === "CHECKPOINTING") { return previousState; }
  if (state !== previousState) { setPreviousState(state); }
  return state;
}
