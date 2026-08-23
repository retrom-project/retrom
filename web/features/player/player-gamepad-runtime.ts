"use client";

import { useEffect, type MutableRefObject } from "react";
import {
  activateControllerFocus,
  changeEditableController,
  controllerBackInScope,
  controllerGroupAction,
  focusControllerDefault,
  moveControllerFocus,
} from "@/features/gamepad/focus-navigation";
import type { ControllerSource } from "@/features/gamepad/types";
import type {
  PlayerControllerAction,
  PlayerGamepadHostControls,
} from "./gamepad-host-controls";

type RuntimeCallbacks = Readonly<{
  claim: (index: number, centerButtonObservable?: boolean) => void;
  onOpenMenu: () => void;
  onOpenExit: () => void;
  onDisconnect: () => void;
  onSuspend: () => void;
  onReconnectReady: () => void;
  onGameplayReady: () => void;
}>;

export function usePlayerGamepadRuntime(
  host: MutableRefObject<PlayerGamepadHostControls>,
  source: ControllerSource,
  callbacks: RuntimeCallbacks,
) {
  useEffect(() => {
    let frame = 0;
    const poll = (now: number) => {
      if (document.visibilityState === "visible" && document.hasFocus()) {
        host.current.sample(source.read(), window.performance.timeOrigin + now);
        host.current.drainActions().forEach((action) => dispatchPlayerAction(action, callbacks));
      }
      frame = window.requestAnimationFrame(poll);
    };
    frame = window.requestAnimationFrame(poll);
    const suspend = () => {
      if (document.visibilityState === "visible" && document.hasFocus()) {return;}
      host.current.suspendUntilNeutral();
      callbacks.onSuspend();
    };
    window.addEventListener("blur", suspend);
    document.addEventListener("visibilitychange", suspend);
    return () => {
      window.cancelAnimationFrame(frame);
      window.removeEventListener("blur", suspend);
      document.removeEventListener("visibilitychange", suspend);
    };
  }, [callbacks, host, source]);
}

function dispatchPlayerAction(action: PlayerControllerAction, callbacks: RuntimeCallbacks) {
  if (action.type === "claimed") {
    callbacks.claim(action.index, action.centerButtonObservable);
    return;
  }
  if (action.type === "open-menu") {callbacks.onOpenMenu(); return;}
  if (action.type === "open-exit-confirmation") {callbacks.onOpenExit(); return;}
  if (action.type === "disconnected") {callbacks.onDisconnect(); return;}
  if (action.type === "ready") {
    callbacks.onReconnectReady();
    window.requestAnimationFrame(() => {
      focusControllerDefault(document.querySelector("[data-player-reconnect='true']") ?? document);
    });
    return;
  }
  if (action.type === "gameplay-ready") {callbacks.onGameplayReady(); return;}
  if (action.type === "confirm") {activateControllerFocus(); return;}
  if (action.type === "back") {controllerBackInScope(); return;}
  if (action.type === "previous-group") {controllerGroupAction(false); return;}
  if (action.type === "next-group") {controllerGroupAction(true); return;}
  if (action.type === "direction" && !changeEditableController(action.direction)) {
    moveControllerFocus(action.direction);
  }
}
