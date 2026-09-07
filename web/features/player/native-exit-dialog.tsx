"use client";

import {useCallback, useEffect, useRef, useState} from "react";
import {ConfirmDialog} from "@/components/confirm-dialog";
import type {GameSaveSync} from "./game-save-sync";
import {ImmersiveMenuInputReader} from "./immersive-controls";

type ExitDraft = Pick<GameSaveSync, "hasChanges" | "save" | "discard">;
type Decision = {native: ExitDraft; hasChanges: boolean; restored: boolean; resolve: (exit: boolean) => void};

export function useNativeExitDecision(getRestored: () => boolean) {
  const [decision, setDecision] = useState<Decision | null>(null);
  const active = useRef<Decision | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState(0);
  const busyRef = useRef(false);
  const pending = useRef<Promise<boolean> | null>(null);
  const decide = useCallback((native: ExitDraft) => {
    if (pending.current) {return pending.current;}
    pending.current = new Promise<boolean>((resolve) => {
      active.current = {native, resolve, hasChanges: native.hasChanges(), restored: getRestored()};
      setDecision(active.current); setError(""); setSelected(0);
    });
    return pending.current;
  }, [getRestored]);
  const finish = useCallback((exit: boolean) => {
    active.current?.resolve(exit); active.current = null; pending.current = null; setDecision(null);
  }, []);
  const choose = useCallback(async (choice: number) => {
    const current = active.current;
    if (!current || busyRef.current) {return;}
    if (choice === 0) {finish(false); return;}
    busyRef.current = true; setBusy(true); setError("");
    try {
      if (choice === 2 && current.hasChanges) {
        if (!await current.native.save()) {setError("保存未成功，草稿已保留。可重试，或返回游戏。"); return;}
      } else if (current.hasChanges) {await current.native.discard();}
      finish(true);
    } catch {setError("操作未完成，本地数据已保留，请重试。");}
    finally {busyRef.current = false; setBusy(false);}
  }, [finish]);
  const chooseAction = useCallback((choice: number) => {void choose(choice);}, [choose]);
  useEffect(() => () => {active.current?.resolve(false); active.current = null;}, []);
  return {decide, dialog: <NativeExitDialog open={decision !== null} hasChanges={decision?.hasChanges ?? false}
    restored={decision?.restored ?? false} busy={busy} error={error}
    selected={selected} onSelect={setSelected} onChoose={chooseAction} />};
}

function NativeExitDialog({open, hasChanges, restored, busy, error, selected, onSelect, onChoose}: {
  open: boolean; hasChanges: boolean; restored: boolean; busy: boolean; error: string; selected: number;
  onSelect: (value: number) => void; onChoose: (value: number) => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const selectedRef = useRef(selected);
  const buttonCount = hasChanges ? 3 : 2;
  useEffect(() => {selectedRef.current = selected;}, [selected]);
  useEffect(() => {
    if (!open) {return;}
    const reader = new ImmersiveMenuInputReader();
    const timer = window.setInterval(() => {
      if (busy) {return;}
      const pads = Array.from(navigator.getGamepads?.() ?? []);
      const index = pads.find((pad) => pad?.connected && pad.mapping === "standard")?.index ?? null;
      const action = reader.update(pads, index, performance.now());
      const buttons = Array.from(root.current?.querySelectorAll(".dialog-actions button") ?? []);
      const focused = buttons.findIndex((button) => button === document.activeElement);
      const current = focused >= 0 ? focused : selectedRef.current;
      if (action === "cancel") {onChoose(0);}
      if (action === "confirm") {onChoose(current);}
      if (action === "left" || action === "right") {onSelect((current + (action === "right" ? 1 : buttonCount - 1)) % buttonCount);}
    }, 16);
    return () => window.clearInterval(timer);
  }, [busy, buttonCount, onChoose, onSelect, open]);
  useEffect(() => {
    if (!open) {return;}
    const buttons = root.current?.querySelectorAll<HTMLButtonElement>(".dialog-actions button");
    buttons?.[selected]?.focus();
  }, [open, selected]);
  return <div ref={root} className="player-native-exit" role="presentation" onKeyDown={(event) => event.stopPropagation()}>
    <ConfirmDialog open={open} title="退出游戏？" busy={busy}
    description={hasChanges ? "当前存档数据已发生变更，是否保存存档？" : "此次游玩似乎没有进行过存档操作，是否继续退出？"}
    secondaryLabel={hasChanges ? "直接退出" : undefined} confirmLabel={hasChanges ? "存档并退出" : "继续退出"}
    cancelLabel="返回游戏" onSecondary={() => onChoose(1)} onConfirm={() => onChoose(hasChanges ? 2 : 1)} onCancel={() => onChoose(0)}>
    <p>{hasChanges ? "注意：请确保此次游玩已在游戏中主动执行过“保存游戏”，避免异常数据变更覆盖此前的存档。"
      : "注意：为避免游戏进度丢失，请确保在退出前已在游戏中主动保存。"}</p>
    {hasChanges ? <p>{restored ? "保存将更新启动时选择的存档。" : "保存后会创建一个新的独立存档。"}
      平台只保存游戏已写入的数据，不保存当前画面的即时进度。</p> : null}
    {error ? <p role="alert">{error}</p> : null}
  </ConfirmDialog></div>;
}
