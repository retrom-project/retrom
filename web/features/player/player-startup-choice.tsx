"use client";

import {useEffect, useCallback, useRef} from "react";
import {useModalFocus} from "@/components/modal-focus";
import {browserGamepadSource} from "@/features/immersive/gamepad-source";
import {GamepadClaimModel, NavigationInputModel, type NavigationAction} from "@/features/immersive/input-model";
import {getActiveImmersiveGamepadIndex, setActiveImmersiveGamepadIndex} from "@/features/immersive/active-gamepad";
import type {RuntimeStartupInteractionV1} from "./runtime/contract";

export function PlayerStartupChoice({interaction, onChoose, onCancel}: {
  interaction: RuntimeStartupInteractionV1;
  onChoose: (id: string) => void;
  onCancel: () => void;
}) {
  const panel = useRef<HTMLElement>(null);
  useModalFocus({open: true, locked: false, panel, onCancel});
  const navigate = useCallback((action: NavigationAction) => {
    const buttons = [...(panel.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? [])];
    if (action === "cancel") {onCancel(); return;}
    const index = buttons.findIndex(button => button === document.activeElement);
    if (action === "confirm") {buttons[index]?.click();}
    if (["up", "down", "left", "right"].includes(action) && buttons.length) {
      const next = (index + (action === "up" || action === "left" ? -1 : 1) + buttons.length) % buttons.length;
      buttons[next]?.focus();
      buttons[next]?.scrollIntoView?.({block: "nearest"});
    }
  }, [onCancel]);
  useEffect(() => {
    const navigation = new NavigationInputModel();
    const claim = new GamepadClaimModel();
    let index = getActiveImmersiveGamepadIndex();
    const stop = browserGamepadSource.subscribe(frame => {
      if (frame.suspended) {navigation.reset(); return;}
      const claimed = claim.update(frame.gamepads);
      if (!frame.gamepads.some(pad => pad.index === index)) {index = null;}
      if (index === null && claimed.claimedIndex !== null) {
        index = claimed.claimedIndex;
        setActiveImmersiveGamepadIndex(index);
      }
      for (const action of navigation.update(frame.gamepads.find(pad => pad.index === index) ?? null, frame.nowMs).actions) {navigate(action);}
    });
    return stop;
  }, [interaction.id, navigate]);
  return <div className="player-loading-layer">
    <section ref={panel} className="player-loading player-startup-choice" tabIndex={-1} role="dialog" aria-modal="true" aria-labelledby="player-startup-title"
      onKeyDown={event => {
        const action = ({ArrowUp: "up", ArrowDown: "down", ArrowLeft: "left", ArrowRight: "right"} as const)[event.key as "ArrowUp"];
        if (action) {event.preventDefault(); event.stopPropagation(); navigate(action);}
      }}>
      <strong id="player-startup-title">{interaction.title}</strong>
      <p>请选择要启动的入口</p>
      <div className="player-startup-options">
        {interaction.choices.map(choice => <button className="button multiline" type="button" key={choice.id} onClick={() => onChoose(choice.id)}>{choice.label}</button>)}
      </div>
      <button className="button" type="button" onClick={onCancel}>取消启动</button>
      <p>方向键 / 手柄选择 · Enter / A 确认 · Escape / B 取消</p>
    </section>
  </div>;
}
