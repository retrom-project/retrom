import type { PlayerGamepadHostBridge } from "./gamepad-host-controls";

function installGetGamepads(runtimeWindow: typeof window, bridge: PlayerGamepadHostBridge) {
  const navigatorValue = runtimeWindow.navigator;
  const originalDescriptor = Object.getOwnPropertyDescriptor(navigatorValue, "getGamepads");
  const original = navigatorValue.getGamepads?.bind(navigatorValue) ?? (() => [] as Gamepad[]);
  const filtered = () => bridge.filterGamepads(
    Array.from(original()),
    runtimeWindow.performance.timeOrigin + runtimeWindow.performance.now(),
  ) as Gamepad[];
  try {
    Object.defineProperty(navigatorValue, "getGamepads", {
      configurable: true,
      enumerable: originalDescriptor?.enumerable ?? false,
      writable: false,
      value: filtered,
    });
  } catch {
    throw new Error("PLAYER_GAMEPAD_FILTER_UNAVAILABLE");
  }
  return () => {
    if (originalDescriptor) {Object.defineProperty(navigatorValue, "getGamepads", originalDescriptor);}
    else {Reflect.deleteProperty(navigatorValue, "getGamepads");}
  };
}

export function installPlayerGamepadFilter(
  runtimeWindow: typeof window,
  bridge: PlayerGamepadHostBridge | undefined,
) {
  if (!bridge) {throw new Error("PLAYER_GAMEPAD_FILTER_UNAVAILABLE");}
  return installGetGamepads(runtimeWindow, bridge);
}
