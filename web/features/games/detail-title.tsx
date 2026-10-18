"use client";
import { useEffect, useRef, useState } from "react";

export function DetailTitle({ title }: { title: string }) {
  const ref = useRef<HTMLHeadingElement>(null);
  const [scrollable, setScrollable] = useState(false);
  useEffect(() => {
    const heading = ref.current!;
    const text = heading.firstElementChild!;
    const reduced = matchMedia("(prefers-reduced-motion: reduce)");
    let distance = 0, frame = 0;
    let hovered = false, focused = false, manual = false;
    const stop = (reset = false) => {
      cancelAnimationFrame(frame);
      if (reset) { heading.scrollLeft = 0; }
    };
    const start = () => {
      stop();
      if (distance <= 0 || reduced.matches || manual) { return; }
      const began = performance.now();
      const travel = distance / 0.035;
      const tick = (now: number) => {
        const elapsed = (now - began) % (travel * 2 + 1800);
        heading.scrollLeft = elapsed < 900 ? 0
          : elapsed < 900 + travel ? (elapsed - 900) * 0.035
          : elapsed < 1800 + travel ? distance
          : distance - (elapsed - 1800 - travel) * 0.035;
        frame = requestAnimationFrame(tick);
      };
      frame = requestAnimationFrame(tick);
    };
    const measure = () => {
      distance = Math.max(0, heading.scrollWidth - heading.clientWidth);
      setScrollable(distance > 1);
      stop(true);
      if (hovered || focused) { start(); }
    };
    const enter = (event: PointerEvent) => {
      if (event.pointerType !== "mouse") { return; }
      hovered = true;
      manual = false;
      start();
    };
    const leave = () => {
      hovered = false;
      if (!focused) { manual = false; stop(true); }
    };
    const focus = () => { focused = true; start(); };
    const blur = () => {
      focused = false;
      if (!hovered) { manual = false; stop(true); }
    };
    const takeOver = () => { manual = true; stop(); };
    const key = (event: KeyboardEvent) => {
      if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) { takeOver(); }
    };
    const observer = new ResizeObserver(measure);
    observer.observe(heading);
    observer.observe(text);
    heading.addEventListener("pointerenter", enter);
    heading.addEventListener("pointerleave", leave);
    heading.addEventListener("pointerdown", takeOver);
    heading.addEventListener("wheel", takeOver, { passive: true });
    heading.addEventListener("keydown", key);
    heading.addEventListener("focus", focus);
    heading.addEventListener("blur", blur);
    reduced.addEventListener("change", measure);
    measure();
    return () => {
      stop();
      observer.disconnect();
      heading.removeEventListener("pointerenter", enter);
      heading.removeEventListener("pointerleave", leave);
      heading.removeEventListener("pointerdown", takeOver);
      heading.removeEventListener("wheel", takeOver);
      heading.removeEventListener("keydown", key);
      heading.removeEventListener("focus", focus);
      heading.removeEventListener("blur", blur);
      reduced.removeEventListener("change", measure);
    };
  }, [title]);
  return <h1 ref={ref} className="game-detail-title" title={title} tabIndex={scrollable ? 0 : undefined}>
    <span>{title}</span>
  </h1>;
}
