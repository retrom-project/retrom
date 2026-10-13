"use client";
import {useEffect, type RefObject} from "react";

/** Keep the editor's scroll areas and close button inside the visible area above the keyboard. */
export function useGameEditorViewport(overlay: RefObject<HTMLElement | null>) {
  useEffect(() => {
    const viewport = window.visualViewport;
    const element = overlay.current;
    if (!viewport || !element) {return;}
    let frame = 0;
    const revealInput = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const focused = document.activeElement;
        if (focused instanceof HTMLElement && element.contains(focused) && focused.matches("input, select, textarea")) {
          focused.scrollIntoView({block: "nearest", inline: "nearest"});
        }
      });
    };
    const update = () => {
      element.style.setProperty("--game-editor-viewport-height", `${viewport.height}px`);
      element.style.setProperty("--game-editor-viewport-top", `${viewport.offsetTop}px`);
      revealInput();
    };
    update();
    viewport.addEventListener("resize", update);
    viewport.addEventListener("scroll", update);
    element.addEventListener("focusin", revealInput);
    return () => {
      viewport.removeEventListener("resize", update);
      viewport.removeEventListener("scroll", update);
      element.removeEventListener("focusin", revealInput);
      cancelAnimationFrame(frame);
    };
  }, [overlay]);
}
