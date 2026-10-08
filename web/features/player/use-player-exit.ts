"use client";
import { useEffect, useRef, useState, type RefObject } from "react";
import type { PlayerRuntimeV1 } from "./runtime/contract";
import { browserGamepadSource } from "@/features/immersive/gamepad-source";
import { buttonPressed } from "@/features/immersive/input-model";
import { ImmersiveNeutralGate } from "./immersive-controls";

export function waitForPlayerNeutral() {
  return new Promise<void>((resolve) => {
    const gate = new ImmersiveNeutralGate();
    const stop = browserGamepadSource.subscribe((frame) => {
      const neutral = frame.gamepads.every((pad) => !pad.buttons.some(buttonPressed) && pad.axes.every((axis) => Math.abs(axis) < .35));
      if (!frame.suspended && gate.update(neutral, frame.nowMs)) { stop(); resolve(); }
    });
  });
}
type Options = {
  runtime: RefObject<PlayerRuntimeV1 | null>;
  immersive: boolean;
  native: boolean;
  hasDraft: boolean;
  saving: boolean;
  retry: (runtime: PlayerRuntimeV1 | null) => Promise<boolean>;
  capture: (runtime: PlayerRuntimeV1, intent: "CAPTURE" | "EXPORT") => Promise<boolean>;
  leave: () => Promise<void>;
  reportError: (message: string) => void;
};
export function usePlayerExit({ runtime, immersive, native, hasDraft, saving, retry, capture, leave, reportError }: Options) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [saveState, setSaveState] = useState<"idle" | "saved" | "error">("idle");
  const pausedByDialog = useRef(false);
  const pending = useRef(false);
  const checkpointWait = useRef<AbortController | null>(null);
  useEffect(() => () => checkpointWait.current?.abort(), []);
  async function request() {
    if (open || pending.current) { return; }
    pending.current = true;
    setBusy(true);
    const waiting = new AbortController();
    checkpointWait.current = waiting;
    try {
      const instance = runtime.current;
      if (instance) { await waitForCheckpoint(instance, waiting.signal); }
      if (waiting.signal.aborted || runtime.current !== instance) { return; }
      pausedByDialog.current = instance?.getState() === "RUNNING" && instance.getCapabilities().pause;
      if (pausedByDialog.current) { await instance?.pause(); }
      setSaveState("idle");
      setOpen(true);
    } catch (failure) { reportError(message(failure, "无法暂停游戏。")); }
    finally { checkpointWait.current = null; pending.current = false; setBusy(false); }
  }
  async function cancel() {
    if (pending.current || saving) { return; }
    pending.current = true;
    setBusy(true);
    try {
      if (immersive) { await waitForPlayerNeutral(); }
      if (immersive && pausedByDialog.current && runtime.current?.getState() === "PAUSED") { await runtime.current.resume(); }
      setOpen(false);
      if (immersive) { requestAnimationFrame(focusGame); }
    } catch (failure) { reportError(message(failure, "无法继续游戏。")); }
    finally { pending.current = false; setBusy(false); }
  }
  async function save() {
    const instance = runtime.current;
    if (!instance || pending.current || saving) { return; }
    pending.current = true;
    setBusy(true);
    try { setSaveState(await (hasDraft ? retry(instance) : capture(instance, native ? "EXPORT" : "CAPTURE")) ? "saved" : "error"); }
    finally { pending.current = false; setBusy(false); }
  }
  async function confirm() {
    if (pending.current || saving) { return; }
    pending.current = true;
    setBusy(true);
    try {
      const instance = runtime.current;
      // A failed native export is presented before a second explicit exit choice.
      if (native && instance && (hasDraft || instance.getCheckpointAvailability().available) && saveState !== "error") {
        if (!await (hasDraft ? retry(instance) : capture(instance, "EXPORT"))) { setSaveState("error"); return; }
      }
      await leave();
    } catch (failure) { reportError(message(failure, "退出游戏失败。")); }
    finally { pending.current = false; setBusy(false); }
  }
  return { open, active: open || busy, opening: busy && !open, busy: busy || saving, saveState, request, cancel, save, confirm };
}
function focusGame() { document.querySelector<HTMLElement>(".player-runtime-mount canvas, .player-runtime-mount iframe")?.focus({ preventScroll: true }); }
function message(failure: unknown, fallback: string) { return failure instanceof Error ? failure.message : fallback; }

function waitForCheckpoint(runtime: PlayerRuntimeV1, signal: AbortSignal) {
  if (runtime.getState() !== "CHECKPOINTING") { return Promise.resolve(); }
  return new Promise<void>((resolve) => {
    let unsubscribe = () => {};
    let finished = false;
    function finish() { finished = true; unsubscribe(); signal.removeEventListener("abort", finish); resolve(); }
    unsubscribe = runtime.subscribe((event) => {
      if ((event.type === "STATE_CHANGED" && event.state !== "CHECKPOINTING") || event.type === "FATAL_ERROR") { finish(); }
    });
    signal.addEventListener("abort", finish, { once: true });
    if (finished || signal.aborted || runtime.getState() !== "CHECKPOINTING") { finish(); }
  });
}
