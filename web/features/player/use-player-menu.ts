"use client";
import { useState } from "react";

/**
 * The Player's more-actions menu. Phones have no toolbar hover, so revealing the toolbar or opening the menu there is
 * a deliberate tap that pauses a running game, as a desktop toolbar click does; tapping the game surface continues.
 */
export function usePlayerMenu({ immersive, desktopPointer, running, onImmersive, onPause }: {
  immersive: boolean;
  desktopPointer: boolean;
  running: () => boolean;
  onImmersive: () => void;
  onPause: () => void;
}) {
  const [menu, setMenu] = useState(false);
  function pauseForTouch() {
    if (!desktopPointer && running()) { onPause(); }
  }
  function toggle() {
    if (immersive) { onImmersive(); return; }
    setMenu(!menu);
    if (!menu) { pauseForTouch(); }
  }
  return { menu, setMenu, toggle, pauseForTouch };
}
