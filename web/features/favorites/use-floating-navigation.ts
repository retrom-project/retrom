"use client";

import { useCallback, useEffect, useRef, type KeyboardEvent, type PointerEvent } from "react";

type Position = { x: number; y: number };

export function constrainNavigation(position: Position, size: { width: number; height: number }, viewport: { width: number; height: number }): Position {
  return {
    x: Math.max(8, Math.min(position.x, viewport.width - size.width - 8)),
    y: Math.max(8, Math.min(position.y, viewport.height - size.height - 8)),
  };
}

export function useFloatingNavigation(collapsed: boolean) {
  const anchor = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLElement>(null);
  const position = useRef<Position | null>(null);
  const drag = useRef<{ pointerId: number; x: number; y: number } | null>(null);
  const layout = useCallback(() => {
    const element = panel.current;
    if (!element || !anchor.current) {return;}
    const origin = anchor.current.getBoundingClientRect();
    const size = element.getBoundingClientRect();
    const next = constrainNavigation(position.current ?? { x: origin.right - size.width, y: origin.top }, size, { width: innerWidth, height: innerHeight });
    if (position.current) {position.current = next;}
    element.style.left = `${next.x}px`;
    element.style.top = `${next.y}px`;
    element.style.visibility = "visible";
  }, []);

  useEffect(() => {
    const observer = new ResizeObserver(layout);
    if (panel.current) {observer.observe(panel.current);}
    if (anchor.current?.parentElement) {observer.observe(anchor.current.parentElement);}
    window.addEventListener("resize", layout);
    window.addEventListener("scroll", layout, true);
    layout();
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", layout);
      window.removeEventListener("scroll", layout, true);
    };
  }, [layout, collapsed]);

  const stop = () => {drag.current = null; panel.current?.classList.remove("is-dragging");};
  const onPointerDown = (event: PointerEvent<HTMLButtonElement>) => {
    if (event.button !== 0 || !event.isPrimary || !panel.current) {return;}
    const rect = panel.current.getBoundingClientRect();
    drag.current = { pointerId: event.pointerId, x: event.clientX - rect.x, y: event.clientY - rect.y };
    event.currentTarget.setPointerCapture(event.pointerId);
    panel.current.classList.add("is-dragging");
  };
  const onPointerMove = (event: PointerEvent<HTMLButtonElement>) => {
    if (!drag.current || event.pointerId !== drag.current.pointerId) {return;}
    position.current = { x: event.clientX - drag.current.x, y: event.clientY - drag.current.y };
    layout();
  };
  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    const delta: Record<string, Position> = { ArrowLeft: { x: -20, y: 0 }, ArrowRight: { x: 20, y: 0 }, ArrowUp: { x: 0, y: -20 }, ArrowDown: { x: 0, y: 20 } };
    const step = delta[event.key];
    if (!step || !panel.current) {return;}
    event.preventDefault();
    const rect = panel.current.getBoundingClientRect();
    position.current = { x: rect.x + step.x, y: rect.y + step.y };
    layout();
  };
  return { anchor, panel, handle: { onPointerDown, onPointerMove, onPointerUp: stop, onPointerCancel: stop, onLostPointerCapture: stop, onKeyDown } };
}
