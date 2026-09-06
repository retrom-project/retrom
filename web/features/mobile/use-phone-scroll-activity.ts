"use client";

import { useEffect, useRef, useState } from "react";
import { usePhoneLayout } from "./phone-layout";

export function usePhoneScrollActivity() {
  const phone = usePhoneLayout();
  const [scrolling, setScrolling] = useState(false);
  const [thumb, setThumb] = useState({ width: 100, left: 0 });
  const timeout = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => () => {
    if (timeout.current !== null) { clearTimeout(timeout.current); }
  }, []);

  function onScroll(element: HTMLDivElement) {
    const { clientWidth, scrollWidth, scrollLeft } = element;
    if (!phone || scrollWidth <= clientWidth) { return; }
    const width = clientWidth / scrollWidth * 100;
    setThumb({ width, left: Math.max(0, Math.min(100 - width, scrollLeft / scrollWidth * 100)) });
    setScrolling(true);
    if (timeout.current !== null) { clearTimeout(timeout.current); }
    timeout.current = setTimeout(() => {
      timeout.current = null;
      setScrolling(false);
    }, 900);
  }

  return { scrolling: phone && scrolling, thumb, onScroll };
}
