"use client";

import {useCallback} from "react";
import type {RuntimeFinalSnapshotV1} from "./runtime/contract";
import type {GameSaveSync} from "./game-save-sync";
import type {RuntimeController} from "./runtime/runtime-controller";

export function usePlayerRuntimeExit(
  controller: {current: RuntimeController | null},
  nativeSave: {current: GameSaveSync | null},
  exit: () => Promise<void>,
  exitStrict: () => Promise<void>,
  exitAfterRuntime: () => Promise<void>,
  showToast: (message: string, timeout?: number) => void,
  decide: (native: GameSaveSync, options?: {canResume: boolean}) => Promise<boolean>,
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
  const exitAfterProviderExit = useCallback(async (snapshot?: RuntimeFinalSnapshotV1) => {
    if (!await finishNativeExit(nativeSave, snapshot, decide)) {return;}
    await nativeSave.current?.stop();
    await controller.current?.exit().catch(() => undefined);
    await exit();
  }, [controller, nativeSave, exit, decide]);
  const exitImmersiveAfterProviderExit = useCallback(async (snapshot?: RuntimeFinalSnapshotV1) => {
    if (!await finishNativeExit(nativeSave, snapshot, decide)) {return;}
    await nativeSave.current?.stop();
    await controller.current?.exit().catch(() => undefined);
    await exitAfterRuntime();
  }, [controller, nativeSave, exitAfterRuntime, decide]);
  return {exitRuntime, exitImmersiveRuntimeStrict, exitImmersiveAfterProviderExit, exitAfterProviderExit};
}

async function prepareNativeExit(controller: {current: RuntimeController | null}, nativeSave: {current: GameSaveSync | null},
  decide: (native: GameSaveSync, options?: {canResume: boolean}) => Promise<boolean>,
) {
  const native = nativeSave.current;
  if (!native) {return true;}
  await controller.current?.runtime.pause();
  try {await native.flush();} catch { /* The dialog allows retry, discard, or continuing. */ }
  return decide(native);
}

async function finishNativeExit(nativeSave: {current: GameSaveSync | null}, snapshot: RuntimeFinalSnapshotV1 | undefined,
  decide: (native: GameSaveSync, options?: {canResume: boolean}) => Promise<boolean>,
) {
  const native = nativeSave.current;
  if (!native) {return true;}
  await native.finish(snapshot);
  return !native.hasChanges() || await decide(native, {canResume: false});
}
