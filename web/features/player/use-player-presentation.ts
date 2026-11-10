"use client";
import {useCallback, useEffect, useRef, useState} from "react";
import {acquirePlayerPresentation, playerLandscapeLockSupported, type PlayerPresentation} from "./player-fullscreen";

/**
 * Rotation and wake-up may lose fullscreen. Automatic retries cover the cases a browser allows without a gesture;
 * a phone that drops out of fullscreen mid-play reports `lost` so the Player can pause and offer a trusted restore.
 */
export function usePlayerPresentation({phone, known, blocked}: {phone: boolean; known: boolean; blocked: boolean}) {
  const [attempted, setAttempted] = useState(false);
  const [pending, setPending] = useState(false);
  const [lost, setLost] = useState(false);
  const active = useRef(false);
  const held = useRef(false);
  const session = useRef<PlayerPresentation | null>(null);
  const operation = useRef<Promise<Element | null> | null>(null);
  const retry = useCallback(() => {
    if (!active.current || !session.current || session.current.stopped || session.current.declined || !phone || !known ||
      document.visibilityState === "hidden" || operation.current || (document.fullscreenElement && !blocked)) {return;}
    const request = session.current.requestFullscreen();
    operation.current = request;
    queueMicrotask(() => {if (active.current && operation.current === request) {setPending(true);}});
    void request.finally(() => {
      if (operation.current === request) {operation.current = null;}
      if (active.current) {setAttempted(true); setPending(false);}
    });
  }, [phone, known, blocked]);
  useEffect(() => {
    active.current = true;
    const scope = acquirePlayerPresentation();
    session.current = scope;
    held.current = document.fullscreenElement !== null;
    // Only a fullscreen this Player actually held can be lost; a windowed start is not an interruption.
    const track = () => {
      if (document.fullscreenElement) {held.current = true; setLost(false); return;}
      setLost(held.current && active.current && !scope.stopped && !scope.declined);
    };
    document.addEventListener("fullscreenchange", track);
    return () => {
      active.current = false;
      document.removeEventListener("fullscreenchange", track);
      // React replays effects in development; only an actual departure releases the scope.
      queueMicrotask(() => {if (!active.current) {void scope.restore();}});
    };
  }, []);
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
  return {
    attempting: phone && known && (!attempted || pending), retry,
    /** The phone left a fullscreen it held; the Player pauses until the user restores or declines it. */
    lost: phone && lost,
    lockable: phone && known && playerLandscapeLockSupported(),
    /** Call from a click: the gesture lets the browser grant fullscreen and the landscape lock. */
    enter: () => {if (session.current) {session.current.declined = false;} retry();},
    decline: () => {if (session.current) {session.current.declined = true;} setLost(false);},
    stop: () => {active.current = false; session.current?.stop();},
    restore: () => session.current?.restore(),
  };
}
