"use client";
import {useCallback, useEffect, useRef, useState} from "react";
import {requestPlayerFullscreen} from "./player-fullscreen";

/** Rotation and wake-up may lose fullscreen; a later trusted surface click retries a denied request. */
export function usePlayerPresentation({phone, known, blocked}: {phone: boolean; known: boolean; blocked: boolean}) {
  const [attempted, setAttempted] = useState(false);
  const [pending, setPending] = useState(false);
  const active = useRef(true);
  const operation = useRef<Promise<Element | null> | null>(null);
  const retry = useCallback(() => {
    if (!phone || !known || document.visibilityState === "hidden" || operation.current ||
      (document.fullscreenElement && !blocked)) {return;}
    const request = requestPlayerFullscreen();
    operation.current = request;
    queueMicrotask(() => {if (active.current && operation.current === request) {setPending(true);}});
    void request.finally(() => {
      if (operation.current === request) {operation.current = null;}
      if (active.current) {setAttempted(true); setPending(false);}
    });
  }, [phone, known, blocked]);
  useEffect(() => {active.current = true; return () => {active.current = false;};}, []);
  useEffect(() => {
    let subscribed = true;
    queueMicrotask(() => {if (subscribed) {retry();}});
    document.addEventListener("visibilitychange", retry);
    window.addEventListener("pageshow", retry);
    window.addEventListener("focus", retry);
    return () => {
      subscribed = false;
      document.removeEventListener("visibilitychange", retry);
      window.removeEventListener("pageshow", retry);
      window.removeEventListener("focus", retry);
    };
  }, [retry]);
  return {attempting: phone && known && (!attempted || pending), retry};
}
