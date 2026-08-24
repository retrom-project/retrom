"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { ImmersiveAudioPreferences } from "./immersive-audio-preferences";

export type ImmersiveBgmState = "playing" | "paused" | "blocked";

export function useImmersiveBackgroundAudio(preferences: ImmersiveAudioPreferences) {
  const audioRef = useRef<HTMLAudioElement>(null);
  const attemptRef = useRef(0);
  const [state, setState] = useState<ImmersiveBgmState>("paused");
  const invalidateAttempts = useCallback(() => {attemptRef.current += 1;}, []);

  const retry = useCallback(async () => {
    const audio = audioRef.current;
    if (!audio || document.visibilityState === "hidden" || audio.muted || audio.volume === 0) {
      setState("paused");
      return false;
    }
    const attempt = ++attemptRef.current;
    try {
      await audio.play();
      if (attempt === attemptRef.current) {setState("playing");}
      return true;
    } catch {
      if (attempt === attemptRef.current) {setState("blocked");}
      return false;
    }
  }, []);

  useEffect(() => {
    const audio = audioRef.current;
    if (!audio) {return;}
    audio.volume = preferences.bgmVolume;
    audio.muted = preferences.bgmMuted;
    if (preferences.bgmMuted || preferences.bgmVolume === 0 || document.visibilityState === "hidden") {
      invalidateAttempts();
      audio.pause();
      const timer = window.setTimeout(() => setState("paused"), 0);
      return () => window.clearTimeout(timer);
    }
    const timer = window.setTimeout(() => void retry(), 0);
    return () => window.clearTimeout(timer);
  }, [invalidateAttempts, preferences.bgmMuted, preferences.bgmVolume, retry]);

  useEffect(() => {
    const visibilityChanged = () => {
      const audio = audioRef.current;
      if (!audio) {return;}
      if (document.visibilityState === "hidden") {
        invalidateAttempts();
        audio.pause();
        setState("paused");
      } else if (!audio.muted && audio.volume > 0) {
        void retry();
      }
    };
    document.addEventListener("visibilitychange", visibilityChanged);
    return () => document.removeEventListener("visibilitychange", visibilityChanged);
  }, [invalidateAttempts, retry]);

  useEffect(() => {
    const audio = audioRef.current;
    return () => {
      invalidateAttempts();
      audio?.pause();
    };
  }, [invalidateAttempts]);

  return { audioRef, markBlocked: () => setState("blocked"), retry, state };
}
