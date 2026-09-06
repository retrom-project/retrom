"use client";

import {useCallback} from "react";
import type {GameSaveSync} from "./game-save-sync";
import type {RuntimeController} from "./runtime/runtime-controller";

export function usePlayerRuntimeExit(
  controller: {current: RuntimeController | null},
  nativeSave: {current: GameSaveSync | null},
  exit: () => Promise<void>,
  exitStrict: () => Promise<void>,
  exitAfterRuntime: () => Promise<void>,
  showToast: (message: string, timeout?: number) => void,
  decide: (native: GameSaveSync) => Promise<boolean>,
) {
  const exitRuntime = useCallback(async () => {
    try {
      if (!await prepareNativeExit(controller, nativeSave, decide)) {
        await controller.current?.runtime.resume(); return;
      }
    } catch {showToast("退出准备失败，游戏数据仍保留，请重试。", 5000); return;}
    await nativeSave.current?.stop();
    await controller.current?.exit().catch(() => undefined);
    await exit();
  }, [controller, nativeSave, exit, showToast, decide]);
  const exitImmersiveRuntimeStrict = useCallback(async () => {
    if (!await prepareNativeExit(controller, nativeSave, decide)) {return false;}
    await nativeSave.current?.stop();
    await controller.current?.exit();
    await exitStrict();
  }, [controller, nativeSave, exitStrict, decide]);
  const exitAfterProviderExit = useCallback(async () => {
    await nativeSave.current?.stop();
    await controller.current?.exit().catch(() => undefined);
    await exit();
  }, [controller, nativeSave, exit]);
  const exitImmersiveAfterProviderExit = useCallback(async () => {
    await nativeSave.current?.stop();
    await controller.current?.exit().catch(() => undefined);
    await exitAfterRuntime();
  }, [controller, nativeSave, exitAfterRuntime]);
  return {exitRuntime, exitImmersiveRuntimeStrict, exitImmersiveAfterProviderExit, exitAfterProviderExit};
}

async function prepareNativeExit(controller: {current: RuntimeController | null}, nativeSave: {current: GameSaveSync | null},
  decide: (native: GameSaveSync) => Promise<boolean>,
) {
  const native = nativeSave.current;
  if (!native) {return true;}
  await controller.current?.runtime.pause();
  try {await native.flush();} catch { /* The dialog allows retry, discard, or continuing. */ }
  return decide(native);
}
