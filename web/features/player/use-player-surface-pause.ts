"use client";
import {useRef, type RefObject} from "react";
import type {PlayerRuntimeV1} from "./runtime/contract";

/** Menu clicks request pause, never toggle it; a surface click waits for that pause before resuming. */
export function usePlayerSurfacePause(runtime: RefObject<PlayerRuntimeV1 | null>, blocked: boolean,
  manualPause: () => void, onError: (message: string) => void) {
  const pending = useRef<{instance: PlayerRuntimeV1 | null; target: "RUNNING" | "PAUSED"; promise: Promise<void>} | null>(null);
  function transition(target: "RUNNING" | "PAUSED") {
    if (blocked) { return Promise.resolve(); }
    manualPause();
    const instance = runtime.current;
    if (pending.current?.instance === instance && pending.current.target === target) { return pending.current.promise; }
    // Native commands acknowledge asynchronously: inspect state after the preceding command completes.
    const preceding = pending.current ? pending.current.promise : Promise.resolve();
    const operation = preceding.then(async () => {
      if (runtime.current !== instance || !instance?.getCapabilities().pause) { return; }
      if (target === "PAUSED" && instance.getState() === "RUNNING") { await instance.pause(); }
      if (target === "RUNNING" && instance.getState() === "PAUSED") { await instance.resume(); }
    }).catch((failure: unknown) => {
      onError(failure instanceof Error ? failure.message : "无法切换游戏暂停状态。");
    }).finally(() => { if (pending.current?.promise === operation) { pending.current = null; } });
    pending.current = {instance, target, promise: operation};
    return operation;
  }
  return {pause: () => transition("PAUSED"), resume: () => transition("RUNNING")};
}
