"use client";

import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import {
  getImmersiveAudioPreferences,
  saveImmersiveAudioPreferences,
  type ImmersiveAudioPreferences,
} from "./immersive-audio-preferences";
import {
  adjustSystemMenuPreference,
  type ImmersiveSystemMenuItem,
  type MenuAdjustment,
} from "./system-menu-model";
import { useImmersiveBackgroundAudio, type ImmersiveBgmState } from "./use-immersive-background-audio";

type ImmersiveAudioContextValue = Readonly<{
  bgmState: ImmersiveBgmState;
  commitPreference: (item: ImmersiveSystemMenuItem, adjustment: MenuAdjustment) => void;
  preferences: ImmersiveAudioPreferences;
  retryBgm: () => void;
}>;

const ImmersiveAudioContext = createContext<ImmersiveAudioContextValue | null>(null);

export function ImmersiveAudioProvider({ children }: { children: ReactNode }) {
  const [preferences, setPreferences] = useState<ImmersiveAudioPreferences>(getImmersiveAudioPreferences);
  const { audioRef, markBlocked, retry, state } = useImmersiveBackgroundAudio(preferences);
  const commitPreference = useCallback((item: ImmersiveSystemMenuItem, adjustment: MenuAdjustment) => {
    setPreferences((current) => {
      const next = adjustSystemMenuPreference(current, item, adjustment);
      if (next !== current) {saveImmersiveAudioPreferences(next);}
      return next;
    });
  }, []);
  const retryBgm = useCallback(() => void retry(), [retry]);
  const value = useMemo(() => ({ bgmState: state, commitPreference, preferences, retryBgm }), [commitPreference, preferences, retryBgm, state]);

  return <ImmersiveAudioContext value={value}>
    {children}
    <audio
      ref={audioRef}
      src="/audio/immersive/insert-coin.ogg"
      data-immersive-bgm="true"
      preload="auto"
      loop
      aria-hidden="true"
      onError={markBlocked}
    />
  </ImmersiveAudioContext>;
}

export function useImmersiveAudioContext() {
  return useContext(ImmersiveAudioContext);
}

export function useImmersiveAudio() {
  const value = useImmersiveAudioContext();
  if (!value) {throw new Error("ImmersiveAudioProvider is required");}
  return value;
}
