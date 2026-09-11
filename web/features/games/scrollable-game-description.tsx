"use client";

import { useEffect, useRef, useState } from "react";

export function ScrollableGameDescription({ description }: { description: string }) {
  const [scrolling, setScrolling] = useState(false);
  const hideTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => {
    if (hideTimer.current !== null) {clearTimeout(hideTimer.current);}
  }, []);
  function revealScrollbar() {
    setScrolling(true);
    if (hideTimer.current !== null) {clearTimeout(hideTimer.current);}
    hideTimer.current = setTimeout(() => setScrolling(false), 700);
  }
  return <div className={`game-detail-description${scrolling ? " is-scrolling" : ""}`} role="region" aria-label="游戏简介" tabIndex={0} onScroll={revealScrollbar}>
    <p>{description.trim() ? description : "尚未填写游戏简介。"}</p>
  </div>;
}
