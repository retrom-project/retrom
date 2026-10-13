"use client";
import { useEffect, useRef } from "react";
import type { RefObject } from "react";

export function PlayerHudHandle({
  visible,
  toolbarRef,
  onReveal,
  desktopPointer,
  onHover,
  onInteract,
}: {
  visible: boolean;
  toolbarRef: RefObject<HTMLElement | null>;
  onReveal: () => void;
  desktopPointer: boolean;
  onHover: (value: boolean) => void;
  onInteract: () => Promise<void>;
}) {
  const keyboardReveal = useRef(false);
  const pointerInteraction = useRef(false);
  useEffect(() => {
    if (visible && keyboardReveal.current) {
      keyboardReveal.current = false;
      toolbarRef.current
        ?.querySelector<HTMLButtonElement>("button:not(:disabled)")
        ?.focus();
    }
  }, [visible, toolbarRef]);
  if (visible && !desktopPointer) {
    return null;
  }
  return (
    <button
      className={`player-hud-handle${desktopPointer ? " is-desktop-pointer" : ""}${visible ? " is-toolbar-visible" : ""}`}
      aria-label="显示游戏工具栏"
      aria-hidden={visible || undefined}
      tabIndex={visible ? -1 : undefined}
      onPointerDown={() => {pointerInteraction.current = true;}}
      onPointerCancel={() => {pointerInteraction.current = false;}}
      onBlur={() => {pointerInteraction.current = false;}}
      onClick={() => { pointerInteraction.current = false; if (visible && desktopPointer) { void onInteract(); } else { onReveal(); } }}
      onPointerEnter={() => { if (desktopPointer) { onHover(true); } }}
      onPointerLeave={() => { if (desktopPointer) { onHover(false); } }}
      onFocus={(event) => {
        if (!pointerInteraction.current && event.currentTarget.matches(":focus-visible")) {
          keyboardReveal.current = true;
          onReveal();
        }
      }}
    >
      <span />
    </button>
  );
}
