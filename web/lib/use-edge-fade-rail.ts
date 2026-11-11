"use client";
import { useCallback, type RefCallback } from "react";
import { listenHorizontalWheel } from "./use-horizontal-wheel";

// Marks which edges hide content so CSS can fade them instead of drawing a scrollbar.
export function listenEdgeFade(rail: HTMLElement) {
  const update = () => {
    const hidden = rail.scrollWidth - rail.clientWidth;
    rail.dataset.fadeStart = String(rail.scrollLeft > 1);
    rail.dataset.fadeEnd = String(hidden - rail.scrollLeft > 1);
  };
  const resize = new ResizeObserver(update);
  const observeChildren = () => {
    resize.disconnect();
    resize.observe(rail);
    for (const child of rail.children) { resize.observe(child); }
    update();
  };
  const mutation = new MutationObserver(observeChildren);
  mutation.observe(rail, { childList: true });
  rail.addEventListener("scroll", update, { passive: true });
  observeChildren();
  return () => {
    rail.removeEventListener("scroll", update);
    mutation.disconnect();
    resize.disconnect();
  };
}

export function useEdgeFadeRail<T extends HTMLElement = HTMLDivElement>(): RefCallback<T> {
  return useCallback((node: T | null) => {
    if (!node) { return undefined; }
    const stopWheel = listenHorizontalWheel(node);
    const stopFade = listenEdgeFade(node);
    return () => { stopWheel(); stopFade(); };
  }, []);
}
