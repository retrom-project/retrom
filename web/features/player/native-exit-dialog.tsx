"use client";

import {useCallback, useEffect, useRef, useState} from "react";
import {ConfirmDialog} from "@/components/confirm-dialog";
import type {GameSaveSync} from "./game-save-sync";
import {ImmersiveMenuInputReader} from "./immersive-controls";
import {storageSaveInstructions} from "./checkpoint-semantics";

type ExitDraft = Pick<GameSaveSync, "hasChanges" | "save" | "discard" | "retry" | "canCapture" | "capture" | "isStorageContainer">;
type Decision = {native: ExitDraft; hasChanges: boolean; canCapture: boolean; storage: boolean; restored: boolean; canResume: boolean; resolve: (exit: boolean) => void};

export function useNativeExitDecision(getRestored: () => boolean) {
  const [decision, setDecision] = useState<Decision | null>(null);
  const active = useRef<Decision | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState(0);
  const busyRef = useRef(false);
  const pending = useRef<Promise<boolean> | null>(null);
  const decide = useCallback((native: ExitDraft, options: {canResume: boolean} = {canResume: true}) => {
    if (pending.current) {return pending.current;}
    pending.current = new Promise<boolean>((resolve) => {
      const canCapture = options.canResume && native.canCapture();
      active.current = {native, resolve, canCapture, storage: native.isStorageContainer(),
        hasChanges: native.hasChanges() || canCapture, restored: getRestored(), canResume: options.canResume};
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
    if (choice === 0 && current.canResume) {finish(false); return;}
    busyRef.current = true; setBusy(true); setError("");
    try {
      if (choice === 0) {
        if (!await current.native.retry()) {setError("草稿尚未存入此浏览器，请重试或选择存档并退出。"); return;}
      } else if (choice === 2 && current.hasChanges) {
        if (!await saveExitData(current)) {setError(current.canResume ? "保存未成功，草稿已保留。可重试，或返回游戏。" : "保存未成功，最终数据已保留。可重试或保留草稿退出。"); return;}
      } else if (current.hasChanges) {await current.native.discard();}
      finish(true);
    } catch {setError("操作未完成，本地数据已保留，请重试。");}
    finally {busyRef.current = false; setBusy(false);}
  }, [finish]);
  const chooseAction = useCallback((choice: number) => {void choose(choice);}, [choose]);
  useEffect(() => () => {active.current?.resolve(false); active.current = null;}, []);
  return {decide, dialog: <NativeExitDialog open={decision !== null} hasChanges={decision?.hasChanges ?? false}
    storage={decision?.storage ?? false}
    canCapture={decision?.canCapture ?? false} restored={decision?.restored ?? false} canResume={decision?.canResume ?? true} busy={busy} error={error}
    selected={selected} onSelect={setSelected} onChoose={chooseAction} />};
}

function NativeExitDialog({open, hasChanges, canCapture, storage, restored, canResume, busy, error, selected, onSelect, onChoose}: {
  open: boolean; hasChanges: boolean; canCapture: boolean; storage: boolean; restored: boolean; canResume: boolean; busy: boolean; error: string; selected: number;
  onSelect: (value: number) => void; onChoose: (value: number) => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const selectedRef = useRef(selected);
  const copy = exitCopy(canCapture, hasChanges, storage);
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
    <ConfirmDialog open={open} title={canResume ? "退出游戏？" : "游戏已结束"} busy={busy}
    description={copy.description}
    secondaryLabel={copy.secondaryLabel} confirmLabel={copy.confirmLabel}
    cancelLabel={canResume ? "返回游戏" : "保留草稿并退出"} onSecondary={() => onChoose(1)} onConfirm={() => onChoose(copy.confirmChoice)} onCancel={() => onChoose(0)}>
    <p>{copy.explanation}</p>
    {hasChanges ? <p>{restored ? "保存将更新启动时选择的存档。" : "保存后会创建一个新的独立存档。"}
      {canCapture ? "存档由游戏自身生成。" : "平台只保存游戏已写入的数据，不保存当前画面的即时进度。"}</p> : null}
    {error ? <p role="alert">{error}</p> : null}
  </ConfirmDialog></div>;
}

function saveExitData(current: Decision) {
  return current.canCapture ? current.native.capture() : current.native.save();
}

function exitCopy(canCapture: boolean, hasChanges: boolean, storage: boolean) {
  if (storage) {
    return {description: hasChanges ? "游戏原生数据已发生变化，是否保存到存档容器？" : "存档容器没有新数据，是否继续退出？",
      explanation: storageSaveInstructions, secondaryLabel: hasChanges ? "直接退出" : undefined,
      confirmLabel: hasChanges ? "存档并退出" : "继续退出", confirmChoice: hasChanges ? 2 : 1};
  }
  return {
    description: canCapture ? "是否保存当前游戏进度后退出？" : hasChanges ? "当前存档数据已发生变更，是否保存存档？" : "此次游玩似乎没有进行过存档操作，是否继续退出？",
    explanation: canCapture ? "将通过游戏自身的存档功能保存当前进度。" : hasChanges ? "注意：请确保此次游玩已在游戏中主动执行过“保存游戏”，避免异常数据变更覆盖此前的存档。"
      : "注意：为避免游戏进度丢失，请确保在退出前已在游戏中主动保存。",
    secondaryLabel: hasChanges ? "直接退出" : undefined, confirmLabel: hasChanges ? "存档并退出" : "继续退出", confirmChoice: hasChanges ? 2 : 1,
  };
}
