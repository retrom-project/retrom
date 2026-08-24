import {
  ImmersiveChordDetector,
  cloneFilteredGamepad,
  gamepadButtonPressed,
  zeroGamepad,
  type GamepadLike,
} from "./immersive-controls";

export type ImmersiveGamepadFilterOptions = {
  activeGamepadIndex: number | null;
  onMenuGesture: () => void;
};

export class ImmersiveGamepadFilter {
  private activeGamepadIndex: number | null;
  private blocked = false;
  private readonly detector = new ImmersiveChordDetector();
  private onMenuGesture: () => void;

  constructor(options: ImmersiveGamepadFilterOptions) {
    this.activeGamepadIndex = options.activeGamepadIndex;
    this.onMenuGesture = options.onMenuGesture;
  }

  setActiveGamepadIndex(index: number | null) {
    if (this.activeGamepadIndex === index) {return;}
    this.activeGamepadIndex = index;
    this.detector.reset();
  }

  setOnMenuGesture(callback: () => void) {this.onMenuGesture = callback;}

  setBlocked(blocked: boolean) {
    this.blocked = blocked;
    if (blocked) {this.detector.reset();}
  }

  reset() {this.detector.reset();}

  filter(gamepads: readonly (GamepadLike | null)[], nowMs: number): (GamepadLike | null)[] {
    if (this.blocked) {return gamepads.map((gamepad) => gamepad ? zeroGamepad(gamepad) : null);}
    const active = this.activeGamepadIndex === null ? null : gamepads[this.activeGamepadIndex];
    if (!active) {this.detector.reset(); return [...gamepads];}
    const output = this.detector.update(
      gamepadButtonPressed(active, 8),
      gamepadButtonPressed(active, 9),
      nowMs,
    );
    if (output.openMenu) {
      this.blocked = true;
      this.onMenuGesture();
      return gamepads.map((gamepad) => gamepad ? zeroGamepad(gamepad) : null);
    }
    return gamepads.map((gamepad) => {
      if (!gamepad || gamepad.index !== this.activeGamepadIndex) {return gamepad;}
      const buttons = [...gamepad.buttons];
      buttons[8] = filteredButton(buttons[8], output.select);
      buttons[9] = filteredButton(buttons[9], output.start);
      return cloneFilteredGamepad(gamepad, buttons, gamepad.axes);
    });
  }
}

function filteredButton(source: GamepadButton | undefined, isPressed: boolean): GamepadButton {
  return {
    pressed: isPressed,
    touched: source?.touched ?? isPressed,
    value: isPressed ? Math.max(0.5, source?.value ?? 1) : 0,
  };
}

export function installImmersiveGamepadFilter(runtimeWindow: Window, filter: ImmersiveGamepadFilter) {
  const gamepadNavigator = runtimeWindow.navigator;
  const ownDescriptor = Object.getOwnPropertyDescriptor(gamepadNavigator, "getGamepads");
  if (ownDescriptor && !ownDescriptor.configurable) {
    throw new Error("PLAYER_IMMERSIVE_GAMEPAD_FILTER_UNAVAILABLE");
  }
  const nativeGetGamepads = gamepadNavigator.getGamepads;
  if (typeof nativeGetGamepads !== "function") {
    throw new Error("PLAYER_IMMERSIVE_GAMEPAD_FILTER_UNAVAILABLE");
  }
  const filteredGetGamepads = () => filter.filter(
    Array.from(nativeGetGamepads.call(gamepadNavigator)),
    runtimeWindow.performance.now(),
  );
  Object.defineProperty(gamepadNavigator, "getGamepads", {
    configurable: true,
    enumerable: ownDescriptor?.enumerable ?? false,
    value: filteredGetGamepads,
    writable: true,
  });
  return () => {
    if (gamepadNavigator.getGamepads !== filteredGetGamepads) {return;}
    if (ownDescriptor) {Object.defineProperty(gamepadNavigator, "getGamepads", ownDescriptor);}
    else {Reflect.deleteProperty(gamepadNavigator, "getGamepads");}
  };
}
