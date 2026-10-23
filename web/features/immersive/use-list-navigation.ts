"use client";
import { useEffect, useRef, useState } from "react";
import type { Resource } from "@/lib/use-resource";
import { letterAt, letterTarget, pageTarget, type JumpAction, type LetterStart } from "./list-jump";
import type { EntryWindow } from "./use-entry-window";

/** `id` changes with every jump, so the list aligns the target once rather than while the hint shows. */
export type JumpHint = { id: number; letter: string; position: string };

/**
 * Moves the selection through a windowed list: single steps cross into earlier or later pages,
 * shoulders page, and triggers or the letter index jump between title letters anywhere in the list.
 */
export function useListNavigation({ entries, letters, selected, setPosition, visibleRows }: {
  entries: EntryWindow;
  letters: Resource<readonly LetterStart[]>;
  selected: number;
  /** Selects a position in the full list; it stays on that game while earlier pages load. */
  setPosition: (position: number) => void;
  visibleRows: () => number;
}) {
  const [hint, setHint] = useState<JumpHint | null>(null);
  const timer = useRef(0);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const total = entries.data?.total ?? 0;

  function show(position: number) {
    window.clearTimeout(timer.current);
    setHint((previous) => ({ id: (previous?.id ?? 0) + 1, letter: letterAt(letters.data ?? [], position) || "#", position: `${position + 1} / ${total}` }));
    timer.current = window.setTimeout(() => setHint(null), 900);
  }

  async function step(direction: "up" | "down") {
    const position = entries.base + selected;
    const loaded = entries.data?.items.length ?? 0;
    if (direction === "up") {
      if (position === 0) { return; }
      if (selected === 0) { await entries.loadBefore(); }
      setPosition(position - 1);
      return;
    }
    const last = entries.base + loaded - 1;
    setPosition(Math.min(position + 1, last + (entries.hasMore ? 1 : 0)));
    if (position + 1 >= last && entries.hasMore && !entries.moreError) { entries.loadMore(); }
  }

  async function moveTo(position: number) {
    if (await entries.moveTo(position) === null) { return; }
    setPosition(position);
    show(position);
  }

  async function page(direction: "previous" | "next") {
    const rows = visibleRows(), position = entries.base + selected;
    if (direction === "previous") {
      const target = Math.max(0, position - rows);
      if (target < entries.base) { await entries.loadBefore(); }
      setPosition(target);
      show(target);
      return;
    }
    const target = pageTarget(selected, rows, direction, entries.data?.items.length ?? 0, entries.hasMore);
    setPosition(entries.base + target.index);
    if (target.loadMore && !entries.moreError) { entries.loadMore(); }
    show(entries.base + target.index);
  }

  async function jump(action: JumpAction) {
    if (!entries.data?.items.length) { return; }
    const direction = action.endsWith("Previous") ? "previous" : "next";
    if (action.startsWith("page")) { await page(direction); return; }
    const target = letterTarget(letters.data ?? [], entries.base + selected, direction);
    if (target !== null) { await moveTo(target); }
  }

  return { hint, step, jump, moveTo };
}
