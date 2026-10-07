import {useLayoutEffect, useRef} from "react";
import type {RuntimeStartupTaskV1} from "./runtime/contract";

/** Slide only when every surviving row advances by the same number of slots. */
export function startupScrollSlots(previous: string[], next: string[]): number {
  if (!previous.length) {return 1;}
  const shifts = next.flatMap((id, index) => {
    const old = previous.indexOf(id);
    return old < 0 ? [] : [next.length - previous.length + old - index];
  });
  return shifts.length && shifts.every(shift => shift === shifts[0]) ? Math.max(0, shifts[0]) : 0;
}

export function useStartupHistoryMotion(tasks: RuntimeStartupTaskV1[]) {
  const ref = useRef<HTMLOListElement>(null);
  const previous = useRef<string[]>([]);
  const animation = useRef<Animation | null>(null);
  useLayoutEffect(() => {
    const list = ref.current;
    const ids = tasks.map(task => task.id);
    if (!list || (ids.length === previous.current.length && ids.every((id, index) => id === previous.current[index]))) {return;}
    const slots = startupScrollSlots(previous.current, ids);
    previous.current = ids;
    if (!list.animate || window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) {return;}
    const transform = getComputedStyle(list).transform;
    const offset = !transform || transform === "none" ? 0 : new DOMMatrixReadOnly(transform).m42;
    animation.current?.cancel();
    const height = list.firstElementChild?.getBoundingClientRect().height ?? 0;
    // One moving list keeps every row the same distance apart, including when
    // a new task arrives before the previous movement finishes.
    const from = Math.min(height * 3, slots * height + offset);
    animation.current = list.animate(slots > 0
      ? [{transform: `translateY(${from}px)`}, {transform: "translateY(0)"}]
      : [{opacity: .65}, {opacity: 1}], {duration: 220, easing: "cubic-bezier(.2,.8,.2,1)"});
  }, [tasks]);
  useLayoutEffect(() => {
    const media = window.matchMedia?.("(prefers-reduced-motion: reduce)");
    const stop = () => animation.current?.cancel();
    media?.addEventListener("change", stop);
    return () => {media?.removeEventListener("change", stop); stop();};
  }, []);
  return ref;
}
