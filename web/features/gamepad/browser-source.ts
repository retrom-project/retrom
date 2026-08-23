import type { ControllerButton, ControllerSnapshot, ControllerSource } from "./types";

function finite(value: number) {
  return Number.isFinite(value) ? value : 0;
}

function normalizeButton(button: GamepadButton): ControllerButton {
  const value = Math.min(1, Math.max(0, finite(button.value)));
  return Object.freeze({
    pressed: button.pressed === true || value >= 0.5,
    touched: button.touched === true,
    value,
  });
}

export function normalizeGamepad(gamepad: Gamepad): ControllerSnapshot {
  return Object.freeze({
    index: gamepad.index,
    connected: gamepad.connected,
    mapping: gamepad.mapping,
    timestamp: finite(gamepad.timestamp),
    buttons: Object.freeze(Array.from(gamepad.buttons, normalizeButton)),
    axes: Object.freeze(Array.from(gamepad.axes, finite)),
  });
}

export function createBrowserControllerSource(navigatorValue: Navigator = navigator): ControllerSource {
  return {
    read() {
      const getGamepads = navigatorValue.getGamepads;
      if (typeof getGamepads !== "function") {return Object.freeze([]);}
      try {
        return Object.freeze(Array.from(getGamepads.call(navigatorValue), (gamepad) =>
          gamepad ? normalizeGamepad(gamepad) : null));
      } catch {
        return Object.freeze([]);
      }
    },
  };
}
