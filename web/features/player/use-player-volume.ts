"use client";
import { useEffect, useEffectEvent, useRef, useState, type RefObject } from "react";
import { getImmersiveAudioPreferences, saveImmersiveAudioPreferences } from "@/features/immersive/immersive-audio-preferences";
import type { PlayerRuntimeV1, RuntimeStateV1 } from "./runtime/contract";

export function usePlayerVolume(runtime: RefObject<PlayerRuntimeV1 | null>, state: RuntimeStateV1, immersive: boolean, onError: (message: string) => void) {
  const [volume, setVolume] = useState(() => {
    const preferences = getImmersiveAudioPreferences();
    return immersive ? preferences.gameMuted ? 0 : preferences.gameVolume : 1;
  });
  const applied = useRef<{ instance: PlayerRuntimeV1; value: number } | null>(null);
  const report = useEffectEvent(onError);
  useEffect(() => {
    const instance = runtime.current;
    if (!instance || !["RUNNING", "PAUSED"].includes(state) || !instance.getCapabilities().volume) { return; }
    if (applied.current?.instance === instance && applied.current.value === volume) { return; }
    applied.current = { instance, value: volume };
    void instance.setVolume(volume).catch((failure: unknown) => {
      applied.current = null;
      report(failure instanceof Error ? failure.message : "无法调整游戏音量。");
    });
  }, [runtime, state, volume]);
  function change(value: number) {
    setVolume(value);
    if (immersive) { saveImmersiveAudioPreferences({ ...getImmersiveAudioPreferences(), gameVolume: value, gameMuted: value === 0 }); }
  }
  return { volume, change };
}
