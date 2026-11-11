"use client";
import { useState, type RefObject } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import type { LaunchEnvelopeV1, PlayerRuntimeV1, RuntimeCheckpointAvailabilityV1, RuntimeFinalSnapshotV1, RuntimeStateV1, RuntimeTouchStickModeV1 } from "./runtime/contract";
import { usePlayerTouchStick } from "./use-player-touch-stick";

/**
 * A new stick mode applies when the game launches. Choosing one asks whether to keep playing or restart now; an
 * instant checkpoint carries the current progress into the relaunch, otherwise the game page reloads.
 */
export function usePlayerStickRestart({ runtime, state, envelope, availability, onOpen, onRestore, onError }: {
  runtime: RefObject<PlayerRuntimeV1 | null>;
  state: RuntimeStateV1;
  envelope: LaunchEnvelopeV1;
  availability: RuntimeCheckpointAvailabilityV1;
  onOpen: () => void;
  onRestore: (snapshot: RuntimeFinalSnapshotV1) => void;
  onError: (message: string) => void;
}) {
  const stick = usePlayerTouchStick(runtime, state, envelope.runtime.coreId);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const keepsProgress = envelope.runtime.checkpoint?.semantics === "INSTANT" && availability.available;
  async function restart() {
    const instance = runtime.current;
    if (!instance || !keepsProgress) {window.location.reload(); return;}
    setBusy(true);
    try {
      const checkpoint = await instance.checkpoint();
      setOpen(false);
      onRestore({ checkpoint, screenshot: null });
    } catch {onError("无法记录当前进度，请先创建存档后再重启游戏。");}
    finally {setBusy(false);}
  }
  return {
    touchStick: stick && { ...stick, toggle: () => {
      const pending = stick.toggle();
      if (pending) {onOpen(); setOpen(true);}
      return pending;
    } },
    dialog: { open: open && stick !== null, mode: stick?.mode ?? "FLOATING", keepsProgress, busy,
      onContinue: () => setOpen(false), onRestart: () => void restart() },
  };
}

/** A stick mode applies from the next launch; the player keeps playing or restarts now. */
export function PlayerStickRestartDialog({ open, mode, keepsProgress, busy, onContinue, onRestart }: {
  open: boolean;
  mode: RuntimeTouchStickModeV1;
  keepsProgress: boolean;
  busy: boolean;
  onContinue: () => void;
  onRestart: () => void;
}) {
  const label = mode === "FLOATING" ? "浮动摇杆" : "固定摇杆";
  const consequence = keepsProgress
    ? "立即重启会保留当前进度，并按新的摇杆模式重新载入游戏。"
    : "立即重启会重新载入游戏，尚未保存的进度会丢失。";
  return <ConfirmDialog open={open} title="需要重启游戏才能生效" description={`已切换为${label}。${consequence}继续游玩则在下次进入游戏时生效。`}
    cancelLabel="继续游玩" confirmLabel="立即重启" busy={busy} interactionDisabled={busy}
    onCancel={onContinue} onConfirm={onRestart} />;
}
