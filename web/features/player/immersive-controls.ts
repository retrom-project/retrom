export const IMMERSIVE_CHORD_WINDOW_MS = 100;
export const IMMERSIVE_CHORD_RELEASE_MS = 60;
export const IMMERSIVE_SECOND_CHORD_MS = 650;
export const IMMERSIVE_NEUTRAL_MS = 120;

type ReservedButton = "select" | "start";

export type GamepadLike = {
  axes: readonly number[];
  buttons: readonly GamepadButton[];
  connected: boolean;
  id: string;
  index: number;
  mapping: GamepadMappingType;
  timestamp: number;
};

export type ReservedButtonOutput = {
  openMenu: boolean;
  select: boolean;
  start: boolean;
};

type Candidate = {
  first: ReservedButton;
  startedAtMs: number;
  secondChord: boolean;
};

function pressed(button: GamepadButton | undefined) {
  return Boolean(button && (button.pressed || button.value >= 0.5));
}

export class ImmersiveChordDetector {
  private previous = { select: false, start: false };
  private downAt: Record<ReservedButton, number | null> = { select: null, start: null };
  private candidate: Candidate | null = null;
  private chord: { secondChord: boolean } | null = null;
  private firstChordCompletedAtMs: number | null = null;
  private lastNowMs = 0;

  reset() {
    this.previous = { select: false, start: false };
    this.downAt = { select: null, start: null };
    this.candidate = null;
    this.chord = null;
    this.firstChordCompletedAtMs = null;
    this.lastNowMs = 0;
  }

  update(select: boolean, start: boolean, nowMs: number): ReservedButtonOutput {
    if (!Number.isFinite(nowMs) || nowMs < this.lastNowMs) {this.reset();}
    this.lastNowMs = nowMs;
    const current = { select, start };
    const rising = {
      select: select && !this.previous.select,
      start: start && !this.previous.start,
    };
    const falling = {
      select: !select && this.previous.select,
      start: !start && this.previous.start,
    };
    this.updateDownTimes(rising, nowMs);
    const output = this.reduceGesture(current, rising, falling, nowMs);
    for (const key of ["select", "start"] as const) {
      if (falling[key]) {this.downAt[key] = null;}
    }
    this.previous = current;
    return output;
  }

  private updateDownTimes(
    rising: Record<ReservedButton, boolean>,
    nowMs: number,
  ) {
    for (const key of ["select", "start"] as const) {
      if (rising[key]) {this.downAt[key] = nowMs;}
    }
  }

  private reduceGesture(
    current: Record<ReservedButton, boolean>,
    rising: Record<ReservedButton, boolean>,
    falling: Record<ReservedButton, boolean>,
    nowMs: number,
  ) {
    if (this.chord) {return this.finishChordWhenReleased(current, nowMs);}
    this.expireFirstChord(nowMs);
    const recognition = this.recognizeChord(current, rising, nowMs);
    if (recognition) {return recognition;}
    this.expireCandidate(current, nowMs);
    return {
      openMenu: false,
      select: this.singleButtonOutput("select", current.select, falling.select, nowMs),
      start: this.singleButtonOutput("start", current.start, falling.start, nowMs),
    };
  }

  private recognizeChord(
    current: Record<ReservedButton, boolean>,
    rising: Record<ReservedButton, boolean>,
    nowMs: number,
  ): ReservedButtonOutput | null {
    if (!this.candidate) {
      const first = rising.select ? "select" : rising.start ? "start" : null;
      if (first) {
        const elapsed = this.firstChordCompletedAtMs === null ? null : nowMs - this.firstChordCompletedAtMs;
        this.candidate = {
          first,
          startedAtMs: nowMs,
          secondChord: elapsed !== null && elapsed >= IMMERSIVE_CHORD_RELEASE_MS && elapsed <= IMMERSIVE_SECOND_CHORD_MS,
        };
      }
    }
    const candidate = this.candidate;
    if (!candidate) {return null;}
    const other: ReservedButton = candidate.first === "select" ? "start" : "select";
    if (!rising[other] || !current[candidate.first] || nowMs - candidate.startedAtMs > IMMERSIVE_CHORD_WINDOW_MS) {
      return null;
    }
    this.chord = { secondChord: candidate.secondChord };
    this.candidate = null;
    this.downAt = { select: null, start: null };
    if (this.chord.secondChord) {this.firstChordCompletedAtMs = null;}
    return { openMenu: this.chord.secondChord, select: false, start: false };
  }

  private finishChordWhenReleased(current: Record<ReservedButton, boolean>, nowMs: number) {
    if (current.select || current.start) {return { openMenu: false, select: false, start: false };}
    if (!this.chord?.secondChord) {this.firstChordCompletedAtMs = nowMs;}
    this.chord = null;
    return { openMenu: false, select: false, start: false };
  }

  private expireFirstChord(nowMs: number) {
    if (this.firstChordCompletedAtMs !== null && nowMs - this.firstChordCompletedAtMs > IMMERSIVE_SECOND_CHORD_MS) {
      this.firstChordCompletedAtMs = null;
    }
  }

  private expireCandidate(current: Record<ReservedButton, boolean>, nowMs: number) {
    const candidate = this.candidate;
    if (!candidate) {return;}
    if (!current[candidate.first] || nowMs - candidate.startedAtMs >= IMMERSIVE_CHORD_WINDOW_MS) {
      this.candidate = null;
    }
  }

  private singleButtonOutput(key: ReservedButton, current: boolean, falling: boolean, nowMs: number) {
    if (this.candidate || this.chord) {return false;}
    const downAt = this.downAt[key];
    if (current && downAt !== null) {return nowMs - downAt >= IMMERSIVE_CHORD_WINDOW_MS;}
    const heldFor = downAt === null ? null : nowMs - downAt;
    return falling && heldFor !== null && heldFor < IMMERSIVE_CHORD_WINDOW_MS;
  }
}

export class ImmersiveNeutralGate {
  private neutralSinceMs: number | null = null;

  reset() {this.neutralSinceMs = null;}

  update(neutral: boolean, nowMs: number) {
    if (!neutral) {this.neutralSinceMs = null; return false;}
    if (this.neutralSinceMs === null) {this.neutralSinceMs = nowMs;}
    return nowMs - this.neutralSinceMs >= IMMERSIVE_NEUTRAL_MS;
  }
}

export type ImmersiveMenuAction = "confirm" | "cancel" | "left" | "right";

export class ImmersiveMenuInputReader {
  private readonly neutralGate = new ImmersiveNeutralGate();
  private previous = { confirm: false, cancel: false, left: false, right: false };
  private ready = false;

  reset() {
    this.neutralGate.reset();
    this.previous = { confirm: false, cancel: false, left: false, right: false };
    this.ready = false;
  }

  update(gamepads: readonly (GamepadLike | null)[], activeIndex: number | null, nowMs: number) {
    if (!this.ready) {
      this.ready = this.neutralGate.update(isNeutralGamepads(gamepads), nowMs);
      return null;
    }
    const gamepad = activeIndex === null ? null : gamepads.find((candidate) => candidate?.index === activeIndex);
    const current = menuButtons(gamepad);
    const action = (Object.keys(current) as ImmersiveMenuAction[]).find((key) => current[key] && !this.previous[key]) ?? null;
    this.previous = current;
    return action;
  }
}

function menuButtons(gamepad: GamepadLike | null | undefined): Record<ImmersiveMenuAction, boolean> {
  const horizontal = gamepad?.axes[0] ?? 0;
  return {
    confirm: gamepadButtonPressed(gamepad, 0),
    cancel: gamepadButtonPressed(gamepad, 1),
    left: gamepadButtonPressed(gamepad, 14) || horizontal <= -0.6,
    right: gamepadButtonPressed(gamepad, 15) || horizontal >= 0.6,
  };
}

export function isNeutralGamepads(gamepads: readonly (GamepadLike | null)[]) {
  return gamepads.every((gamepad) => !gamepad || gamepad.buttons.every((button) => !pressed(button)) &&
    gamepad.axes.every((axis) => Number.isFinite(axis) && Math.abs(axis) < 0.35));
}

export function isStandardImmersiveGamepad(gamepad: GamepadLike | null | undefined): gamepad is Gamepad {
  return Boolean(gamepad?.connected && gamepad.mapping === "standard" && gamepad.buttons.length > 15 &&
    [0, 1, 8, 9, 12, 13, 14, 15].every((index) => {
      const button = gamepad.buttons[index];
      return button && Number.isFinite(button.value);
    }) && gamepad.axes.every(Number.isFinite));
}

export function gamepadButtonPressed(gamepad: GamepadLike | null | undefined, index: number) {
  return pressed(gamepad?.buttons[index]);
}

export function cloneFilteredGamepad(
  gamepad: GamepadLike,
  buttons: readonly GamepadButton[],
  axes: readonly number[],
) {
  return {
    axes: [...axes],
    buttons: buttons.map((button) => ({ pressed: button.pressed, touched: button.touched, value: button.value })),
    connected: gamepad.connected,
    id: gamepad.id,
    index: gamepad.index,
    mapping: gamepad.mapping,
    timestamp: gamepad.timestamp,
  };
}

export function zeroGamepad(gamepad: GamepadLike) {
  const buttons = gamepad.buttons.map(() => ({ pressed: false, touched: false, value: 0 }));
  return cloneFilteredGamepad(gamepad, buttons, gamepad.axes.map(() => 0));
}
