export type GamepadButtonSnapshot = Readonly<{ pressed: boolean; value: number }>;

export type GamepadSnapshot = Readonly<{
  axes: readonly number[];
  buttons: readonly GamepadButtonSnapshot[];
  connected: boolean;
  index: number;
  mapping: string;
}>;

export type NavigationAction = "left" | "right" | "up" | "down" | "confirm" | "cancel" | "menu" | "favorite";

export type NavigationUpdate = Readonly<{
  actions: readonly NavigationAction[];
  neutral: boolean;
  neutralReady: boolean;
}>;

const REQUIRED_BUTTON_COUNT = 16;
const BUTTON_THRESHOLD = 0.5;
const AXIS_ENTER_THRESHOLD = 0.6;
const AXIS_EXIT_THRESHOLD = 0.35;
const INITIAL_REPEAT_MS = 350;
const REPEAT_MS = 120;

function finite(values: readonly number[]) {
  return values.every(Number.isFinite);
}

export function isStandardGamepad(gamepad: GamepadSnapshot) {
  return gamepad.connected
    && gamepad.mapping === "standard"
    && gamepad.buttons.length >= REQUIRED_BUTTON_COUNT
    && gamepad.buttons.every((button) => Number.isFinite(button.value))
    && finite(gamepad.axes);
}

export function buttonPressed(button: GamepadButtonSnapshot | undefined) {
  return Boolean(button?.pressed || (button?.value ?? 0) >= BUTTON_THRESHOLD);
}

function axisValue(gamepad: GamepadSnapshot, index: number) {
  const value = gamepad.axes[index];
  return Number.isFinite(value) ? value : 0;
}

function isNeutral(gamepad: GamepadSnapshot | null) {
  if (!gamepad) {return false;}
  return !gamepad.buttons.some(buttonPressed)
    && Math.abs(axisValue(gamepad, 0)) < AXIS_EXIT_THRESHOLD
    && Math.abs(axisValue(gamepad, 1)) < AXIS_EXIT_THRESHOLD;
}

type Direction = Extract<NavigationAction, "left" | "right" | "up" | "down">;

function digitalDirection(gamepad: GamepadSnapshot): Direction | null {
  const horizontal = buttonPressed(gamepad.buttons[14]) ? -1 : buttonPressed(gamepad.buttons[15]) ? 1 : 0;
  const vertical = buttonPressed(gamepad.buttons[12]) ? -1 : buttonPressed(gamepad.buttons[13]) ? 1 : 0;
  if (horizontal !== 0 && vertical !== 0) {return horizontal < 0 ? "left" : "right";}
  if (horizontal !== 0) {return horizontal < 0 ? "left" : "right";}
  if (vertical !== 0) {return vertical < 0 ? "up" : "down";}
  return null;
}

function axisDirection(gamepad: GamepadSnapshot, previous: Direction | null): Direction | null {
  const horizontal = axisValue(gamepad, 0);
  const vertical = axisValue(gamepad, 1);
  const largest = Math.max(Math.abs(horizontal), Math.abs(vertical));
  const entered = largest >= AXIS_ENTER_THRESHOLD
    ? Math.abs(horizontal) >= Math.abs(vertical) ? horizontal < 0 ? "left" : "right" : vertical < 0 ? "up" : "down"
    : null;
  if (entered && entered !== previous) {return entered;}
  if (!previous) {return entered;}
  const previousValue = previous === "left" || previous === "right" ? horizontal : vertical;
  const sameSign = previous === "left" || previous === "up" ? previousValue < 0 : previousValue > 0;
  return sameSign && Math.abs(previousValue) >= AXIS_EXIT_THRESHOLD ? previous : entered;
}

function risingButtonAction(pressed: readonly boolean[], previous: readonly boolean[]): NavigationAction | null {
  if (pressed[1] && !previous[1]) {return "cancel";}
  if (pressed[0] && !previous[0]) {return "confirm";}
  if (pressed[8] && !previous[8]) {return "menu";}
  if (pressed[3] && !previous[3]) {return "favorite";}
  return null;
}

export class NavigationInputModel {
  private previousButtons: boolean[] = [];
  private direction: Direction | null = null;
  private nextRepeatAtMs = 0;
  private neutralSinceMs: number | null = null;
  private neutralDelayMs = 120;
  private armed = false;
  private directionBeforeNeutralEnabled: boolean;

  constructor(allowDirectionBeforeNeutral = false) {
    this.directionBeforeNeutralEnabled = allowDirectionBeforeNeutral;
  }

  reset(neutralDelayMs = 120) {
    this.previousButtons = [];
    this.direction = null;
    this.nextRepeatAtMs = 0;
    this.neutralSinceMs = null;
    this.neutralDelayMs = neutralDelayMs;
    this.armed = false;
  }

  update(gamepad: GamepadSnapshot | null, nowMs: number): NavigationUpdate {
    if (!gamepad || !isStandardGamepad(gamepad)) {
      this.reset(this.neutralDelayMs);
      return { actions: [], neutral: false, neutralReady: false };
    }
    const neutral = isNeutral(gamepad);
    if (!this.armed && !neutral) {this.neutralSinceMs = null;}
    else if (!this.armed && this.neutralSinceMs === null) {this.neutralSinceMs = nowMs;}
    if (!this.armed && this.neutralSinceMs !== null && nowMs - this.neutralSinceMs >= this.neutralDelayMs) {
      this.armed = true;
      this.directionBeforeNeutralEnabled = false;
    }
    const neutralReady = this.armed;
    const pressed = gamepad.buttons.map(buttonPressed);
    const actions = neutralReady || this.directionBeforeNeutralEnabled
      ? this.actionsFor(gamepad, pressed, nowMs, neutralReady)
      : [];
    this.previousButtons = pressed;
    return { actions, neutral, neutralReady };
  }

  private actionsFor(gamepad: GamepadSnapshot, pressed: boolean[], nowMs: number, buttonsAllowed = true) {
    const actions: NavigationAction[] = [];
    const nextDirection = digitalDirection(gamepad) ?? axisDirection(gamepad, this.direction);
    if (!nextDirection) {
      this.direction = null;
      this.nextRepeatAtMs = 0;
    } else if (nextDirection !== this.direction) {
      this.direction = nextDirection;
      this.nextRepeatAtMs = nowMs + INITIAL_REPEAT_MS;
      actions.push(nextDirection);
    } else if (nowMs >= this.nextRepeatAtMs) {
      actions.push(nextDirection);
      this.nextRepeatAtMs = nowMs + REPEAT_MS;
    }
    const buttonAction = buttonsAllowed ? risingButtonAction(pressed, this.previousButtons) : null;
    if (buttonAction) {actions.push(buttonAction);}
    return actions;
  }
}

export type ClaimResult = Readonly<{ claimedIndex: number | null; unsupportedEdge: boolean }>;

export class GamepadClaimModel {
  private previous = new Map<number, boolean[]>();

  reset(gamepads: readonly GamepadSnapshot[] = []) {
    this.previous = new Map(gamepads.map((gamepad) => [gamepad.index, gamepad.buttons.map(buttonPressed)]));
  }

  update(gamepads: readonly GamepadSnapshot[]): ClaimResult {
    let claimedIndex: number | null = null;
    let unsupportedEdge = false;
    for (const gamepad of [...gamepads].sort((left, right) => left.index - right.index)) {
      const previous = this.previous.get(gamepad.index) ?? [];
      const pressed = gamepad.buttons.map(buttonPressed);
      const edge = pressed.some((value, index) => value && !previous[index]);
      if (edge && isStandardGamepad(gamepad) && claimedIndex === null) {claimedIndex = gamepad.index;}
      if (edge && !isStandardGamepad(gamepad)) {unsupportedEdge = true;}
      this.previous.set(gamepad.index, pressed);
    }
    const connected = new Set(gamepads.map((gamepad) => gamepad.index));
    for (const index of this.previous.keys()) {
      if (!connected.has(index)) {this.previous.delete(index);}
    }
    return { claimedIndex, unsupportedEdge };
  }
}
