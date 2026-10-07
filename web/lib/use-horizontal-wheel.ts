"use client";
import { useCallback, type RefCallback } from "react";

function hasVerticalScroller(event: WheelEvent, area: HTMLElement) {
  for (const target of event.composedPath()) {
    if (!(target instanceof HTMLElement)) { continue; }
    if (target.scrollHeight > target.clientHeight &&
      ["auto", "scroll"].includes(getComputedStyle(target).overflowY)) { return true; }
    if (target === area) { break; }
  }
  return false;
}

export function listenHorizontalWheel(area: HTMLElement, scroller: HTMLElement = area) {
  function onWheel(event: WheelEvent) {
    if (event.defaultPrevented || !event.cancelable || event.ctrlKey || event.metaKey ||
      event.shiftKey || hasVerticalScroller(event, area)) { return; }
    // A custom scrollbar beside its scroller has no native horizontal target.
    // Gestures inside the actual scroller retain the browser's own handling.
    const delta = event.deltaX !== 0
      ? area !== scroller && !event.composedPath().includes(scroller) ? event.deltaX : 0
      : event.deltaY;
    if (delta === 0) { return; }
    const maximum = scroller.scrollWidth - scroller.clientWidth;
    if (maximum <= 0) { return; }
    const scale = event.deltaMode === WheelEvent.DOM_DELTA_LINE ? 16
      : event.deltaMode === WheelEvent.DOM_DELTA_PAGE ? scroller.clientWidth : 1;
    const next = Math.max(0, Math.min(maximum, scroller.scrollLeft + delta * scale));
    if (next === scroller.scrollLeft) { return; }
    scroller.scrollLeft = next;
    event.preventDefault();
  }
  area.addEventListener("wheel", onWheel, { passive: false });
  return () => area.removeEventListener("wheel", onWheel);
}

export function useHorizontalWheel<T extends HTMLElement = HTMLDivElement>(): RefCallback<T> {
  return useCallback((node: T | null) => node ? listenHorizontalWheel(node) : undefined, []);
}
