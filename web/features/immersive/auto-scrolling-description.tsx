"use client";

import { useEffect, useRef } from "react";

const START_PAUSE_MS = 1_200;
const END_PAUSE_MS = 1_800;
const SCROLL_PIXELS_PER_MS = 0.026;

export function AutoScrollingDescription({ className, text }: { className: string; text: string }) {
  const descriptionRef = useRef<HTMLParagraphElement>(null);

  useEffect(() => {
    const description = descriptionRef.current;
    if (!description) {return;}
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
    let animationFrame = 0;
    let lastTimestamp = 0;
    let pauseUntil = 0;
    let scrollPosition = 0;
    let waitingAtEnd = false;
    description.scrollTop = 0;

    const animate = (timestamp: number) => {
      if (!lastTimestamp) {
        lastTimestamp = timestamp;
        pauseUntil = timestamp + START_PAUSE_MS;
      }
      const maximumScroll = Math.max(0, description.scrollHeight - description.clientHeight);
      if (reducedMotion.matches || document.visibilityState === "hidden" || maximumScroll === 0) {
        lastTimestamp = timestamp;
        animationFrame = window.requestAnimationFrame(animate);
        return;
      }
      if (timestamp >= pauseUntil && waitingAtEnd) {
        scrollPosition = 0;
        description.scrollTop = 0;
        waitingAtEnd = false;
        pauseUntil = timestamp + START_PAUSE_MS;
      } else if (timestamp >= pauseUntil) {
        const elapsed = Math.min(100, timestamp - lastTimestamp);
        scrollPosition = Math.min(maximumScroll, scrollPosition + elapsed * SCROLL_PIXELS_PER_MS);
        description.scrollTop = scrollPosition;
        if (scrollPosition >= maximumScroll - 0.5) {
          scrollPosition = maximumScroll;
          description.scrollTop = maximumScroll;
          waitingAtEnd = true;
          pauseUntil = timestamp + END_PAUSE_MS;
        }
      }
      lastTimestamp = timestamp;
      animationFrame = window.requestAnimationFrame(animate);
    };
    const resetForMotionPreference = () => {
      scrollPosition = 0;
      if (reducedMotion.matches) {description.scrollTop = scrollPosition;}
      lastTimestamp = 0;
      waitingAtEnd = false;
    };

    reducedMotion.addEventListener("change", resetForMotionPreference);
    animationFrame = window.requestAnimationFrame(animate);
    return () => {
      window.cancelAnimationFrame(animationFrame);
      reducedMotion.removeEventListener("change", resetForMotionPreference);
    };
  }, [text]);

  return <p
    ref={descriptionRef}
    className={className}
    data-auto-scroll="true"
    data-immersive-description="true"
    aria-label={text}
  >{text}</p>;
}
