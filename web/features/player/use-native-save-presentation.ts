"use client";

import {useCallback, useState, type Dispatch, type SetStateAction} from "react";
import type {GameSavePresentation} from "./game-save-sync";
import type {NativeSaveCapabilities} from "./checkpoint-semantics";

export function useNativeSavePresentation(
  availableRef: {current: boolean}, setAvailable: Dispatch<SetStateAction<boolean>>,
  setText: Dispatch<SetStateAction<string>>, setTone: Dispatch<SetStateAction<GameSavePresentation["tone"]>>,
) {
  const [nativeSave, setNativeSave] = useState<NativeSaveCapabilities>();
  const [nativeRetryAvailable, setNativeRetryAvailable] = useState(false);
  const presentGameSave = useCallback((value: GameSavePresentation) => {
    availableRef.current = value.available; setAvailable(value.available);
    setNativeSave(value.save); setNativeRetryAvailable(value.retryAvailable);
    setText(value.text); setTone(value.tone);
  }, [availableRef, setAvailable, setText, setTone]);
  return {nativeSave, nativeRetryAvailable, presentGameSave};
}
