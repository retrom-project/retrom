"use client";

import {useCallback, useMemo, useRef, useState} from "react";
import type {PlayerRuntimeV1, RuntimeGamepadCursorV1} from "./runtime/contract";
import {readGamepadCursorPreference, readPlayerGame, writeGamepadCursorPreference} from "./gamepad-cursor-preference";

export type PlayerGamepadCursorControl = {enabled: boolean; toggle: () => void};
export const gamepadCursorInstructions = "方向键 / 左摇杆移动 · A 左键（按住拖动）· B 右键 · LB 慢速";

export function useGamepadCursor(userId: string | undefined, launchId: string, showToast: (message: string) => void) {
  const cursor = useRef<RuntimeGamepadCursorV1 | null>(null);
  const [enabled, setEnabled] = useState<boolean | null>(null);
  const initialize = useCallback((runtime: Pick<PlayerRuntimeV1, "getGamepadCursor">) => {
    cursor.current = runtime.getGamepadCursor?.() ?? null;
    if (!cursor.current) {setEnabled(null); return;}
    const preference = readGamepadCursorPreference(userId, readPlayerGame(launchId));
    if (preference !== null) {cursor.current.setEnabled(preference);}
    setEnabled(cursor.current.getState().enabled);
  }, [launchId, userId]);
  const toggle = useCallback(() => {
    const active = cursor.current;
    if (!active) {return;}
    try {
      const next = !active.getState().enabled;
      active.setEnabled(next);
      setEnabled(next);
      writeGamepadCursorPreference(userId, readPlayerGame(launchId), next);
      showToast(next ? `手柄光标已开启。${gamepadCursorInstructions}` : "手柄光标已关闭");
    } catch {showToast("无法切换手柄光标，请稍后重试。");}
  }, [launchId, showToast, userId]);
  const control = useMemo<PlayerGamepadCursorControl | null>(
    () => enabled === null ? null : {enabled, toggle}, [enabled, toggle],
  );
  return {initialize, control};
}
