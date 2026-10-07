"use client";
import { useEffect, useRef } from "react";
import type { RefObject } from "react";

export function PlayerHudHandle({
  visible,
  toolbarRef,
  onReveal,
}: {
  visible: boolean;
  toolbarRef: RefObject<HTMLElement | null>;
  onReveal: () => void;
}) {
  const keyboardReveal = useRef(false);
  useEffect(() => {
    if (visible && keyboardReveal.current) {
      keyboardReveal.current = false;
      toolbarRef.current
        ?.querySelector<HTMLButtonElement>("button:not(:disabled)")
        ?.focus();
    }
  }, [visible, toolbarRef]);
  if (visible) {
    return null;
  }
  return (
    <button
      className="player-hud-handle"
      aria-label="显示游戏工具栏"
      onClick={onReveal}
      onFocus={(event) => {
        if (event.currentTarget.matches(":focus-visible")) {
          keyboardReveal.current = true;
          onReveal();
        }
      }}
    >
      <span />
    </button>
  );
}
