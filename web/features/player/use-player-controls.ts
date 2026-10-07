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
import { navigatePlayerMenu } from "./player-menu-navigation";

export function usePlayerControls(
  runtime: RefObject<PlayerRuntimeV1 | null>,
  onMenu: () => void,
  onPause: () => void,
  suppressInput: boolean,
  onFailure: (message: string) => void,
  menuOpen: boolean,
) {
  const config = useRef({
    onMenu,
    onPause,
    suppressInput,
    onFailure,
    menuOpen,
  });
  useEffect(() => {
    config.current = { onMenu, onPause, suppressInput, onFailure, menuOpen };
  }, [onMenu, onPause, suppressInput, onFailure, menuOpen]);
  useEffect(() => {
    let index = getActiveImmersiveGamepadIndex();
    let previous = false;
    let filtered = "";
    let filteredInstance: PlayerRuntimeV1 | null = null;
    const claim = new GamepadClaimModel();
    const navigation = new NavigationInputModel();
    function keyboard(event: KeyboardEvent) {
      handleKeyboard(event, runtime.current, config.current);
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
      previous = dispatchGamepadMenu(
        gamepad,
        frame.nowMs,
        navigation,
        previous,
        current,
        instance.getInputCapabilities().hostShortcuts.includes("MENU"),
      );
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
  config: { menuOpen: boolean; onMenu: () => void },
  menuSupported: boolean,
) {
  const pressed = buttonPressed(gamepad?.buttons[8]);
  if (config.menuOpen) {
    for (const action of navigation.update(gamepad, nowMs).actions) {
      if (action === "cancel" || action === "menu") {
        config.onMenu();
      } else {
        navigatePlayerMenu(action);
      }
    }
  } else {
    navigation.reset();
    if (pressed && !previous && menuSupported) {
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
  config: {
    onMenu: () => void;
    onPause: () => void;
    menuOpen: boolean;
  },
) {
  if (!instance || !["RUNNING", "PAUSED"].includes(instance.getState())) {
    return;
  }
  const shortcuts = instance.getInputCapabilities().hostShortcuts;
  if (event.key === "Escape" && shortcuts.includes("MENU")) {
    event.preventDefault();
    config.onMenu();
  } else if (event.key === "Pause" && shortcuts.includes("PAUSE")) {
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
