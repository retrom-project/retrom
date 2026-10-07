"use client";
import { useEffect, useRef } from "react";
import type { RefObject } from "react";
import type { PlayerRuntimeV1 } from "./runtime/contract";
import { browserGamepadSource } from "@/features/immersive/gamepad-source";
import {
  getActiveImmersiveGamepadIndex,
  setActiveImmersiveGamepadIndex,
} from "@/features/immersive/active-gamepad";
import {
  buttonPressed,
  GamepadClaimModel,
  NavigationInputModel,
} from "@/features/immersive/input-model";
import type { GamepadSnapshot } from "@/features/immersive/input-model";
import { ImmersiveChordDetector } from "./immersive-controls";
import { navigatePlayerMenu } from "./player-menu-navigation";

type ControlConfig = {
  suppressInput: boolean;
  onFailure: (message: string) => void;
  menuOpen: boolean;
  immersive: boolean;
  dialogOpen: boolean;
  onCancel: () => void;
};
export function usePlayerControls(runtime: RefObject<PlayerRuntimeV1 | null>, onMenu: () => void, onPause: () => void, options: ControlConfig) {
  const config = useRef({ onMenu, onPause, ...options });
  useEffect(() => { config.current = { onMenu, onPause, ...options }; }, [onMenu, onPause, options]);
  useEffect(() => {
    let index = getActiveImmersiveGamepadIndex();
    let previous = false;
    let filtered = "";
    let filteredInstance: PlayerRuntimeV1 | null = null;
    const claim = new GamepadClaimModel();
    const navigation = new NavigationInputModel();
    const chord = new ImmersiveChordDetector();
    function keyboard(event: KeyboardEvent) {
      if (!keyboardOwnedByOverlay(event, config.current)) { handleKeyboard(event, runtime.current, config.current); }
    }
    const unsubscribe = browserGamepadSource.subscribe((frame) => {
      const instance = runtime.current;
      if (!instance || !["RUNNING", "PAUSED"].includes(instance.getState())) {
        return;
      }
      if (frame.suspended) {
        navigation.reset();
        previous = true;
        filtered = "";
        applyFilter(instance, index, true, config.current.onFailure);
        return;
      }
      const claimed = claim.update(frame.gamepads);
      if (!frame.gamepads.some((gamepad) => gamepad.index === index)) {
        index = null;
      }
      if (index === null && claimed.claimedIndex !== null) {
        index = claimed.claimedIndex;
        setActiveImmersiveGamepadIndex(index);
      }
      const current = config.current;
      const filterKey = `${index}:${current.suppressInput}`;
      if (instance !== filteredInstance || filterKey !== filtered) {
        filteredInstance = instance;
        filtered = filterKey;
        applyFilter(instance, index, current.suppressInput, current.onFailure);
      }
      const gamepad =
        frame.gamepads.find((item) => item.index === index) ?? null;
      if (current.immersive) {
        dispatchImmersiveGamepad(gamepad, frame.nowMs, navigation, chord, current);
      } else {
        previous = dispatchGamepadMenu(gamepad, frame.nowMs, navigation, previous, current, instance.getInputCapabilities().hostShortcuts.includes("MENU"));
      }
    });
    window.addEventListener("keydown", keyboard, true);
    return () => {
      unsubscribe();
      window.removeEventListener("keydown", keyboard, true);
    };
  }, [runtime]);
}

function dispatchGamepadMenu(
  gamepad: GamepadSnapshot | null,
  nowMs: number,
  navigation: NavigationInputModel,
  previous: boolean,
  config: ControlConfig & { onMenu: () => void },
  menuSupported: boolean,
) {
  const pressed = buttonPressed(gamepad?.buttons[8]);
  if (config.dialogOpen) {
    dispatchDialog(navigation.update(gamepad, nowMs).actions, config.onCancel);
  } else if (config.menuOpen) {
    for (const action of navigation.update(gamepad, nowMs).actions) {
      if (action === "cancel" || action === "menu") {
        config.onMenu();
      } else {
        navigatePlayerMenu(action);
      }
    }
  } else {
    navigation.reset();
    if (pressed && !previous && menuSupported && !config.suppressInput) {
      config.onMenu();
    }
  }
  return pressed;
}

function applyFilter(
  runtime: PlayerRuntimeV1,
  index: number | null,
  suppressInput: boolean,
  onFailure: (message: string) => void,
) {
  if (!runtime.getCapabilities().inputFilter) {
    return;
  }
  void runtime
    .setInputFilter({ activeGamepadIndex: index, suppressInput })
    .catch((failure: unknown) =>
      onFailure(
        failure instanceof Error ? failure.message : "无法应用输入设置。",
      ),
    );
}

function handleKeyboard(
  event: KeyboardEvent,
  instance: PlayerRuntimeV1 | null,
  config: ControlConfig & { onMenu: () => void; onPause: () => void },
) {
  if (!instance || !["RUNNING", "PAUSED"].includes(instance.getState())) {
    return;
  }
  const shortcuts = instance.getInputCapabilities().hostShortcuts;
  if (config.immersive) {
    if (event.key.toLowerCase() === "m" && shortcuts.includes("MENU")) { event.preventDefault(); config.onMenu(); }
    return;
  }
  if (event.key === "Escape" && shortcuts.includes("MENU")) {
    event.preventDefault();
    config.onMenu();
  } else if ((event.key === "Pause" || event.code === "KeyP") && shortcuts.includes("PAUSE")) {
    event.preventDefault();
    config.onPause();
  } else if (config.menuOpen) {
    const action = menuKeys[event.key];
    if (action) {
      event.preventDefault();
      navigatePlayerMenu(action);
    }
  }
}
const menuKeys: Record<string, "up" | "down" | "left" | "right" | "confirm"> = {
  ArrowUp: "up",
  ArrowDown: "down",
  ArrowLeft: "left",
  ArrowRight: "right",
  Enter: "confirm",
};

function dispatchImmersiveGamepad(gamepad: GamepadSnapshot | null, nowMs: number, navigation: NavigationInputModel, chord: ImmersiveChordDetector, config: ControlConfig & { onMenu: () => void }) {
  if (config.dialogOpen) {
    chord.reset();
    dispatchDialog(navigation.update(gamepad, nowMs).actions, config.onCancel);
    return;
  }
  navigation.reset();
  if (!config.suppressInput && chord.update(buttonPressed(gamepad?.buttons[8]), buttonPressed(gamepad?.buttons[9]), nowMs).openMenu) { config.onMenu(); }
}
function dispatchDialog(actions: readonly string[], onCancel: () => void) {
  const panel = document.activeElement?.closest<HTMLElement>('[role="dialog"], [role="alertdialog"]') ?? document.querySelector<HTMLElement>('.player-shell [role="dialog"], .player-shell [role="alertdialog"]') ?? document.querySelector<HTMLElement>('[role="dialog"], [role="alertdialog"]');
  if (!panel) { return; }
  const buttons = [...panel.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled)')];
  for (const action of actions) {
    if (action === "cancel") {
      const escape = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true });
      panel.dispatchEvent(escape);
      if (!escape.defaultPrevented) { onCancel(); }
      continue;
    }
    const active = document.activeElement;
    if (action === "confirm" && active instanceof HTMLButtonElement && buttons.includes(active)) { active.click(); }
    if (["left", "right", "up", "down"].includes(action)) {
      const index = buttons.indexOf(active as HTMLButtonElement);
      const direction = action === "left" || action === "up" ? -1 : 1;
      buttons[(index + direction + buttons.length) % buttons.length]?.focus();
    }
  }
}

function keyboardOwnedByOverlay(event: KeyboardEvent, config: ControlConfig) {
  if (event.defaultPrevented || config.dialogOpen || (config.suppressInput && !config.menuOpen)) { return true; }
  if (event.repeat || event.isComposing || event.ctrlKey || event.altKey || event.metaKey) { return true; }
  const target = event.target as { closest?: (selector: string) => Element | null } | null;
  return typeof target?.closest === "function" && !!target.closest("input, select, textarea, [contenteditable=true]");
}
