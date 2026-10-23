"use client";
import { useEffect, useRef, type RefObject } from "react";
import type { LetterStart } from "./list-jump";
import styles from "./list-jump.module.css";

/** Loads the page before the window when the top of the list scrolls into view. */
export function EarlierEntries({ base, loading, onLoad, scrollRoot }: {
  base: number;
  loading: boolean;
  onLoad: () => void;
  scrollRoot: RefObject<HTMLElement | null>;
}) {
  const marker = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const element = marker.current;
    if (!element || base === 0 || loading || !globalThis.IntersectionObserver) { return; }
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) { onLoad(); }
    }, { root: scrollRoot.current, rootMargin: "240px 0px 0px 0px" });
    observer.observe(element);
    return () => observer.disconnect();
  }, [base, loading, onLoad, scrollRoot]);
  if (base === 0) { return null; }
  return <div className={styles.earlier} ref={marker}>
    <button type="button" className="button secondary" disabled={loading} onClick={onLoad}>
      {loading ? "正在加载…" : `前面还有 ${base} 款游戏`}
    </button>
  </div>;
}

/** Letters present in the title-ordered list; choosing one opens the list at that letter. */
export function LetterIndex({ letters, current, onPick }: {
  letters: readonly LetterStart[];
  current: string;
  onPick: (offset: number) => void;
}) {
  if (letters.length < 2) { return null; }
  return <nav className={styles.letterIndex} aria-label="按字母跳转">
    {letters.map((letter) => <button key={letter.letter} type="button" aria-current={letter.letter === current ? "true" : undefined}
      title={`${letter.letter} · ${letter.count} 款游戏`} onClick={() => onPick(letter.offset)}>{letter.letter}</button>)}
  </nav>;
}
