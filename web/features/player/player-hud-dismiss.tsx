"use client";
import type {RuntimeStateV1} from "./runtime/contract";

/** A real surface click dismisses controls without confusing iframe autofocus with user input. */
export function PlayerHudDismiss({visible, enabled, state, immersive, onDismiss}: {
  visible: boolean; enabled: boolean; state: RuntimeStateV1; immersive: boolean; onDismiss: () => void;
}) {
  if (!visible || !enabled || immersive || state !== "RUNNING") {return null;}
  return <button className="player-hud-dismiss" aria-label="收起游戏工具栏" onClick={onDismiss} />;
}
