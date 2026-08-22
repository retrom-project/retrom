export type EmulatorControlBinding = {
  value: string;
  value2?: string;
};

export type EmulatorDefaultControls = Record<number, Record<number, EmulatorControlBinding>>;

const controlCount = 30;

const playerOneKeyboard: Readonly<Record<number, string>> = {
  0: "j",
  1: "l",
  2: "5",
  3: "1",
  4: "w",
  5: "s",
  6: "a",
  7: "d",
  8: "k",
  9: "i",
};

const playerTwoKeyboard: Readonly<Record<number, string>> = {
  0: "numpad 1",
  1: "numpad 3",
  // Coin is shared through P1 control 2. EmulatorJS dispatches every same-key
  // match, so binding P2 here would inject two coin slots for one key press.
  3: "2",
  4: "up arrow",
  5: "down arrow",
  6: "left arrow",
  7: "right arrow",
  8: "numpad 2",
  9: "numpad 5",
};

// These are the pinned EmulatorJS 4.2.3/4.3 defaults. Keyboard overrides must
// retain them byte-for-byte so connecting a gamepad behaves as before.
const playerOneGamepad: Readonly<Record<number, string>> = {
  0: "BUTTON_2",
  1: "BUTTON_4",
  2: "SELECT",
  3: "START",
  4: "DPAD_UP",
  5: "DPAD_DOWN",
  6: "DPAD_LEFT",
  7: "DPAD_RIGHT",
  8: "BUTTON_1",
  9: "BUTTON_3",
  10: "LEFT_TOP_SHOULDER",
  11: "RIGHT_TOP_SHOULDER",
  12: "LEFT_BOTTOM_SHOULDER",
  13: "RIGHT_BOTTOM_SHOULDER",
  14: "LEFT_STICK",
  15: "RIGHT_STICK",
  16: "LEFT_STICK_X:+1",
  17: "LEFT_STICK_X:-1",
  18: "LEFT_STICK_Y:+1",
  19: "LEFT_STICK_Y:-1",
  20: "RIGHT_STICK_X:+1",
  21: "RIGHT_STICK_X:-1",
  22: "RIGHT_STICK_Y:+1",
  23: "RIGHT_STICK_Y:-1",
};

export function createRetromDefaultControls(): EmulatorDefaultControls {
  const controllers: EmulatorDefaultControls = {};
  for (let player = 0; player < 4; player += 1) {
    const keyboard: Readonly<Record<number, string>> = player === 0
      ? playerOneKeyboard
      : player === 1 ? playerTwoKeyboard : {};
    const controls: Record<number, EmulatorControlBinding> = {};
    for (let control = 0; control < controlCount; control += 1) {
      const value2 = player === 0 ? playerOneGamepad[control] : undefined;
      controls[control] = {
        value: keyboard[control] ?? "",
        ...(value2 ? { value2 } : {}),
      };
    }
    controllers[player] = controls;
  }
  return controllers;
}

type ClosestTarget = EventTarget & {
  closest: (selectors: string) => Element | null;
};

function hasClosest(target: EventTarget | null): target is ClosestTarget {
  return typeof target === "object" && target !== null && "closest" in target &&
    typeof target.closest === "function";
}

export function handlePlayerPauseShortcut(event: KeyboardEvent, onPause: () => void) {
  if (event.code !== "KeyP" || event.repeat || event.isComposing || event.ctrlKey || event.altKey || event.metaKey) {
    return false;
  }
  if (hasClosest(event.target) && event.target.closest("input,select,textarea,[contenteditable=true]")) {
    return false;
  }
  event.preventDefault();
  event.stopImmediatePropagation();
  onPause();
  return true;
}
