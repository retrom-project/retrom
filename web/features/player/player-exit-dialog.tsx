"use client";
import { useRef, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { useModalFocus } from "@/components/modal-focus";
import type { usePlayerExit } from "./use-player-exit";

type Props = {
  immersive: boolean;
  native: boolean;
  available: boolean;
  draft: boolean;
  controller: ReturnType<typeof usePlayerExit>;
};
export function PlayerExitDialog(props: Props) {
  const { immersive, native, available, draft, controller } = props;
  const description = exitDescription(props);
  const saveLabel = draft ? "重试同步存档" : saveActionLabel(native, controller.saveState);
  const disabled = !draft && (!available || (!native && controller.saveState === "saved"));
  return immersive ? <ImmersivePlayerMenu open={controller.open} busy={controller.busy} description={immersiveDescription(props)} saveLabel={saveLabel} saveDisabled={disabled} onCancel={() => void controller.cancel()} onSave={() => void controller.save()} onExit={() => void controller.confirm()} /> : <ConfirmDialog open={controller.open} title="退出游戏？" description={draft ? "当前仍有未同步的存档草稿。" : native ? "退出前将检查游戏内存档的同步状态。" : "直接退出不会创建存档；如需保留当前位置，请先创建存档。"} confirmLabel="退出游戏" tone="danger" leadingLabel={saveLabel} leadingDisabled={disabled} leadingBusy={controller.busy} leadingBusyLabel={native ? "正在同步…" : "正在保存…"} onLeading={() => void controller.save()} onCancel={() => void controller.cancel()} onConfirm={() => void controller.confirm()} interactionDisabled={controller.busy}><span>{description}</span></ConfirmDialog>;
}
function exitDescription({ draft, native, available, controller }: Props) {
  if (draft) { return "存档尚未同步。退出后草稿会保留在这个浏览器，可在我的存档中重新同步或导出。"; }
  if (native && controller.saveState === "error") { return "游戏已写入的数据未能同步。可重试同步；再次选择退出游戏将结束本次游戏，未导出的数据可能丢失。"; }
  if (native) { return "请先在游戏中执行保存。平台仅同步游戏已写入的数据，不保存当前画面的即时进度；退出前会检查同步状态。"; }
  if (!available && controller.saveState !== "saved") { return "当前无法创建存档，退出将结束本次游玩。"; }
  return "只有点击“创建存档”才会保存当前位置；直接退出只结束本次游戏，未保存的进度不会保留。";
}
function ImmersivePlayerMenu({ open, busy, description, saveLabel, saveDisabled, onCancel, onSave, onExit }: { open: boolean; busy: boolean; description: string; saveLabel: string; saveDisabled: boolean; onCancel: () => void; onSave: () => void; onExit: () => void }) {
  const panel = useRef<HTMLElement>(null);
  const initial = useRef<HTMLButtonElement>(null);
  const [selected, setSelected] = useState(0);
  useModalFocus({ open, locked: busy, panel, initial, onCancel });
  if (!open) { return null; }
  const actions = [{ label: "取消", disabled: false, onClick: onCancel }, { label: saveLabel, disabled: saveDisabled, onClick: onSave }, { label: "退出游戏", disabled: false, onClick: onExit }];
  return <div className="immersive-player-overlay"><section ref={panel} className="immersive-player-panel" role="dialog" aria-modal="true" aria-labelledby="immersive-player-title" aria-describedby="immersive-player-description" tabIndex={-1} onKeyDown={(event) => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") { return; }
    event.preventDefault();
    const choices = [...(panel.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? [])];
    const index = choices.indexOf(document.activeElement as HTMLButtonElement);
    choices[(index + (event.key === "ArrowLeft" ? -1 : 1) + choices.length) % choices.length]?.focus();
  }}><p className="immersive-player-eyebrow">游戏已暂停</p><h1 id="immersive-player-title">游戏菜单</h1><p id="immersive-player-description">{description}</p><div className="immersive-player-actions">{actions.map((action, index) => <button key={index} ref={index === 0 ? initial : undefined} className={selected === index ? "is-selected" : ""} disabled={busy || action.disabled} onFocus={() => setSelected(index)} onClick={action.onClick}>{action.label}</button>)}</div><small>A 确认 · B 取消</small></section></div>;
}

function saveActionLabel(native: boolean, state: "idle" | "saved" | "error") {
  if (native) { return state === "saved" ? "已同步存档" : state === "error" ? "重试同步存档" : "同步存档"; }
  return state === "saved" ? "已创建存档" : state === "error" ? "重试创建存档" : "创建存档";
}

function immersiveDescription(props: Props) {
  return !props.native && !props.draft && props.available ? "可以在这里创建存档；退出游戏不会自动保存当前进度。" : exitDescription(props);
}
