"use client";

import {useCallback, useEffect, useRef, useState} from "react";
import {ConfirmDialog} from "@/components/confirm-dialog";
import type {GameSaveSync} from "./game-save-sync";
import {gameSaveInstructions} from "./checkpoint-semantics";
import {ImmersiveMenuInputReader} from "./immersive-controls";

type ExitDraft = Pick<GameSaveSync, "hasChanges" | "save" | "discard">;
type Decision = {native: ExitDraft; restored: boolean; resolve: (exit: boolean) => void};

export function useNativeExitDecision(getRestored: () => boolean) {
  const [decision, setDecision] = useState<Decision | null>(null);
  const active = useRef<Decision | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState(0);
  const busyRef = useRef(false);
  const pending = useRef<Promise<boolean> | null>(null);
  const decide = useCallback((native: ExitDraft) => {
    if (!native.hasChanges()) {return Promise.resolve(true);}
    if (pending.current) {return pending.current;}
    pending.current = new Promise<boolean>((resolve) => {
      active.current = {native, resolve, restored: getRestored()}; setDecision(active.current); setError(""); setSelected(0);
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
      if (choice === 1) {
        if (!await current.native.save()) {setError("保存未成功，草稿已保留。可重试，或继续游戏。"); return;}
      } else {await current.native.discard();}
      finish(true);
    } catch {setError("操作未完成，本地数据已保留，请重试。");}
    finally {busyRef.current = false; setBusy(false);}
  }, [finish]);
  const chooseAction = useCallback((choice: number) => {void choose(choice);}, [choose]);
  useEffect(() => () => {active.current?.resolve(false); active.current = null;}, []);
  return {decide, dialog: <NativeExitDialog open={decision !== null} restored={decision?.restored ?? false} busy={busy} error={error}
    selected={selected} onSelect={setSelected} onChoose={chooseAction} />};
}

function NativeExitDialog({open, restored, busy, error, selected, onSelect, onChoose}: {
  open: boolean; restored: boolean; busy: boolean; error: string; selected: number;
  onSelect: (value: number) => void; onChoose: (value: number) => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const selectedRef = useRef(selected);
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
      const current = [1, 0, 2][focused] ?? selectedRef.current;
      if (action === "cancel") {onChoose(0);}
      if (action === "confirm") {onChoose(current);}
      if (action === "left" || action === "right") {onSelect((current + (action === "right" ? 2 : 1)) % 3);}
    }, 16);
    return () => window.clearInterval(timer);
  }, [busy, onChoose, onSelect, open]);
  useEffect(() => {
    if (!open) {return;}
    const buttons = root.current?.querySelectorAll<HTMLButtonElement>(".dialog-actions button");
    buttons?.[selected === 0 ? 1 : selected === 1 ? 0 : 2]?.focus();
  }, [open, selected]);
  return <div ref={root} className="player-native-exit" role="presentation" onKeyDown={(event) => event.stopPropagation()}>
    <ConfirmDialog open={open} title="游戏存档数据存在变更" busy={busy}
    description={restored ? "本次数据只暂存在此浏览器。保存将更新启动时选择的存档；如果误选了“开始新游戏”，请选择“不保存并退出”。" : "本次数据只暂存在此浏览器。保存后会创建一个新的独立存档。"}
    leadingLabel="保存并退出" confirmLabel="不保存并退出" cancelLabel="继续游戏" tone="danger"
    onLeading={() => onChoose(1)} onConfirm={() => onChoose(2)} onCancel={() => onChoose(0)}>
    <p>{gameSaveInstructions}</p>{error ? <p role="alert">{error}</p> : null}
  </ConfirmDialog></div>;
}
