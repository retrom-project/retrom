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
) {
  const exitRuntime = useCallback(async () => {
    try {await flushNativeSave(nativeSave);}
    catch {showToast("游戏数据尚未同步，退出已取消。请重试同步后退出。", 5000); return;}
    await nativeSave.current?.stop();
    await controller.current?.exit().catch(() => undefined);
    await exit();
  }, [controller, nativeSave, exit, showToast]);
  const exitImmersiveRuntimeStrict = useCallback(async () => {
    await flushNativeSave(nativeSave);
    await nativeSave.current?.stop();
    await controller.current?.exit();
    await exitStrict();
  }, [controller, nativeSave, exitStrict]);
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

async function flushNativeSave(nativeSave: {current: GameSaveSync | null}) {
  if (!nativeSave.current) {return;}
  await nativeSave.current.flush();
}
