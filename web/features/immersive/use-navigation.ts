"use client";
import { useEffect, useRef, useState, type RefObject } from "react";
import { browserGamepadSource } from "./gamepad-source";
import { GamepadClaimModel, NavigationInputModel, isStandardGamepad } from "./input-model";
import type { NavigationAction } from "./input-model";
import {
  getActiveImmersiveGamepadIndex,
  isImmersivePlayerReturnPending,
  consumeImmersivePlayerReturn,
  setActiveImmersiveGamepadIndex,
} from "./active-gamepad";
export function useImmersiveNavigation(
  onAction: (action: NavigationAction) => void,
  returnFocus?: RefObject<HTMLElement | null>,
) {
  const action = useRef(onAction);
  useEffect(() => {
    action.current = onAction;
  }, [onAction]);
  const [controller, setController] = useState<number | null>(() =>
    getActiveImmersiveGamepadIndex(),
  );
  const [message, setMessage] = useState("");
  const [keyboardReady, setKeyboardReady] = useState(isImmersivePlayerReturnPending);
  useEffect(() => {
    if (consumeImmersivePlayerReturn()) {
      // Removing the focused runtime iframe can leave the Host document unfocused.
      // Focus its new browsing surface without activating another browser window.
      returnFocus?.current?.focus({ preventScroll: true });
    }
    const claim = new GamepadClaimModel();
    const navigation = new NavigationInputModel();
    let index = getActiveImmersiveGamepadIndex();
    const unsubscribe = browserGamepadSource.subscribe((frame) => {
      if (frame.suspended) {
        navigation.reset();
        claim.reset();
        return;
      }
      if (
        index !== null &&
        !frame.gamepads.some((pad) => pad.index === index && isStandardGamepad(pad))
      ) {
        index = null;
        setController(null);
        setKeyboardReady(false);
        setActiveImmersiveGamepadIndex(null);
        setMessage("手柄已断开。连接后按任意按钮继续。");
      }
      if (index === null) {
        const result = claim.update(frame.gamepads);
        if (result.unsupportedEdge) {
          setMessage("此手柄尚未提供标准按键映射，请使用键盘或鼠标。");
        }
        if (result.claimedIndex !== null) {
          index = result.claimedIndex;
          setController(index);
          setActiveImmersiveGamepadIndex(index);
          setMessage("");
          navigation.reset();
        }
        return;
      }
      const update = navigation.update(
        frame.gamepads.find((pad) => pad.index === index) ?? null,
        frame.nowMs,
      );
      for (const input of update.actions) {
        action.current(input);
      }
    });
    function keyboard(event: KeyboardEvent) {
      if (
        event.defaultPrevented ||
        (event.target instanceof HTMLElement && event.target.closest('[role="dialog"], [role="alertdialog"]') && ["Tab", "Enter"].includes(event.key)) ||
        event.target instanceof HTMLInputElement ||
        event.target instanceof HTMLSelectElement ||
        event.target instanceof HTMLTextAreaElement
      ) {
        return;
      }
      const input = keys[event.key];
      if (input) {
        event.preventDefault();
        setKeyboardReady(true);
        action.current(input);
      }
    }
    window.addEventListener("keydown", keyboard);
    return () => {
      unsubscribe();
      window.removeEventListener("keydown", keyboard);
    };
  }, [returnFocus]);
  return { controller, message, ready: controller !== null || keyboardReady };
}
const keys: Record<string, NavigationAction> = {
  ArrowLeft: "left",
  ArrowRight: "right",
  ArrowUp: "up",
  ArrowDown: "down",
  Enter: "confirm",
  Escape: "cancel",
  s: "menu",
  S: "menu",
  y: "favorite",
  Y: "favorite",
};
