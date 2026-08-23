import type {
  ControllerAction,
  ControllerDirection,
  ControllerSnapshot,
} from "./types";

const BUTTON_THRESHOLD = 0.5;
const AXIS_ENTER = 0.6;
const AXIS_RELEASE = 0.35;
const AXIS_TIE = 0.1;
const NEUTRAL_GATE_MS = 120;
const REPEAT_DELAY_MS = 360;
const REPEAT_INTERVAL_MS = 110;

type AxisState = Readonly<{ direction: ControllerDirection | null; primary: "x" | "y" | null }>;

export type NavigationControllerState = Readonly<{
  activeIndex: number | null;
  phase: "unclaimed" | "neutral-gate" | "ready";
  neutralSince: number | null;
  buttons: readonly boolean[];
  axis: AxisState;
  heldDirection: ControllerDirection | null;
  repeatAt: number | null;
  centerButtonObservable: boolean;
}>;

export type NavigationControllerResult = Readonly<{
  state: NavigationControllerState;
  actions: readonly ControllerAction[];
}>;

export function initialNavigationControllerState(activeIndex: number | null = null): NavigationControllerState {
  return {
    activeIndex,
    phase: activeIndex === null ? "unclaimed" : "neutral-gate",
    neutralSince: null,
    buttons: Object.freeze([]),
    axis: { direction: null, primary: null },
    heldDirection: null,
    repeatAt: null,
    centerButtonObservable: false,
  };
}

function buttonPressed(snapshot: ControllerSnapshot, index: number) {
  const button = snapshot.buttons[index];
  return Boolean(button && (button.pressed || button.value >= BUTTON_THRESHOLD));
}

function validButton(snapshot: ControllerSnapshot, index: number) {
  const button = snapshot.buttons[index];
  return Boolean(button && Number.isFinite(button.value));
}

export function isStandardNavigationController(snapshot: ControllerSnapshot) {
  if (!snapshot.connected || snapshot.mapping !== "standard" || !Number.isInteger(snapshot.index)) {return false;}
  if (![0, 1, 8, 9].every((index) => validButton(snapshot, index))) {return false;}
  const hasDpad = [12, 13, 14, 15].every((index) => validButton(snapshot, index));
  const hasStick = snapshot.axes.length >= 2 && snapshot.axes.slice(0, 2).every(Number.isFinite);
  return hasDpad || hasStick;
}

export function isControllerNeutral(snapshot: ControllerSnapshot) {
  return snapshot.buttons.every((button) => !button.pressed && button.value < BUTTON_THRESHOLD) &&
    snapshot.axes.slice(0, 2).every((axis) => Math.abs(axis) < AXIS_RELEASE);
}

function hasClaimInput(snapshot: ControllerSnapshot) {
  return snapshot.buttons.some((button) => button.pressed || button.value >= BUTTON_THRESHOLD) ||
    snapshot.axes.slice(0, 2).some((axis) => Math.abs(axis) >= AXIS_ENTER);
}

function findController(
  snapshots: readonly (ControllerSnapshot | null)[],
  index: number | null,
) {
  return index === null ? null : snapshots.find((snapshot) => snapshot?.index === index) ?? null;
}

function findClaim(snapshotList: readonly (ControllerSnapshot | null)[]) {
  return snapshotList.find((snapshot): snapshot is ControllerSnapshot =>
    Boolean(snapshot && isStandardNavigationController(snapshot) && hasClaimInput(snapshot))) ?? null;
}

function directionForDpad(snapshot: ControllerSnapshot): ControllerDirection | null {
  if (buttonPressed(snapshot, 12)) {return "up";}
  if (buttonPressed(snapshot, 13)) {return "down";}
  if (buttonPressed(snapshot, 14)) {return "left";}
  if (buttonPressed(snapshot, 15)) {return "right";}
  return null;
}

function retainedAxisDirection(snapshot: ControllerSnapshot, previous: AxisState): AxisState {
  const x = snapshot.axes[0] ?? 0;
  const y = snapshot.axes[1] ?? 0;
  if (previous.direction && Math.abs(previous.primary === "x" ? x : y) > AXIS_RELEASE) {return previous;}
  if (Math.abs(x) < AXIS_ENTER && Math.abs(y) < AXIS_ENTER) {return { direction: null, primary: null };}
  let primary: "x" | "y";
  if (Math.abs(Math.abs(x) - Math.abs(y)) < AXIS_TIE && previous.primary) {primary = previous.primary;}
  else {primary = Math.abs(x) > Math.abs(y) ? "x" : "y";}
  if (primary === "x") {return { direction: x < 0 ? "left" : "right", primary };}
  return { direction: y < 0 ? "up" : "down", primary };
}

function readDirection(snapshot: ControllerSnapshot, previous: AxisState) {
  const dpad = directionForDpad(snapshot);
  if (dpad) {return { direction: dpad, axis: { direction: null, primary: null } as AxisState };}
  const axis = retainedAxisDirection(snapshot, previous);
  return { direction: axis.direction, axis };
}

function buttonActions(snapshot: ControllerSnapshot, previous: readonly boolean[]) {
  const pressed = snapshot.buttons.map((_, index) => buttonPressed(snapshot, index));
  const rising = (index: number) => pressed[index] === true && previous[index] !== true;
  const actions: ControllerAction[] = [];
  if (rising(0)) {actions.push({ type: "confirm" });}
  if (rising(1)) {actions.push({ type: "back" });}
  if (rising(4)) {actions.push({ type: "previous-group" });}
  if (rising(5)) {actions.push({ type: "next-group" });}
  if (rising(9) || rising(16) || rising(8) && pressed[9]) {actions.push({ type: "navigation" });}
  return { pressed: Object.freeze(pressed), actions };
}

function directionActions(
  direction: ControllerDirection | null,
  held: ControllerDirection | null,
  repeatAt: number | null,
  now: number,
) {
  if (!direction) {return { actions: [] as ControllerAction[], heldDirection: null, repeatAt: null };}
  if (direction !== held) {
    return {
      actions: [{ type: "direction", direction }] as ControllerAction[],
      heldDirection: direction,
      repeatAt: now + REPEAT_DELAY_MS,
    };
  }
  if (repeatAt !== null && now >= repeatAt) {
    return {
      actions: [{ type: "direction", direction }] as ControllerAction[],
      heldDirection: direction,
      repeatAt: now + REPEAT_INTERVAL_MS,
    };
  }
  return { actions: [] as ControllerAction[], heldDirection: held, repeatAt };
}

function claimController(state: NavigationControllerState, claim: ControllerSnapshot): NavigationControllerResult {
  const centerButtonObservable = validButton(claim, 16);
  return {
    state: {
      ...initialNavigationControllerState(claim.index),
      centerButtonObservable,
    },
    actions: [{ type: "claimed", index: claim.index, centerButtonObservable }],
  };
}

function disconnectedController(state: NavigationControllerState): NavigationControllerResult {
  return {
    state: initialNavigationControllerState(),
    actions: state.activeIndex === null ? [] : [{ type: "disconnected", index: state.activeIndex }],
  };
}

function passNeutralGate(
  state: NavigationControllerState,
  snapshot: ControllerSnapshot,
  now: number,
): NavigationControllerResult {
  if (!isControllerNeutral(snapshot)) {
    return { state: { ...state, neutralSince: null }, actions: [] };
  }
  const neutralSince = state.neutralSince ?? now;
  if (now - neutralSince < NEUTRAL_GATE_MS) {
    return { state: { ...state, neutralSince }, actions: [] };
  }
  return {
    state: { ...state, phase: "ready", neutralSince, buttons: Object.freeze(snapshot.buttons.map(() => false)) },
    actions: [{ type: "ready", index: snapshot.index }],
  };
}

export function updateNavigationController(
  state: NavigationControllerState,
  snapshots: readonly (ControllerSnapshot | null)[],
  now: number,
): NavigationControllerResult {
  if (state.activeIndex === null) {
    const claim = findClaim(snapshots);
    return claim ? claimController(state, claim) : { state, actions: [] };
  }
  const active = findController(snapshots, state.activeIndex);
  if (!active?.connected || !isStandardNavigationController(active)) {return disconnectedController(state);}
  if (state.phase === "neutral-gate") {return passNeutralGate(state, active, now);}
  const buttons = buttonActions(active, state.buttons);
  const directional = readDirection(active, state.axis);
  const repeated = directionActions(directional.direction, state.heldDirection, state.repeatAt, now);
  return {
    state: {
      ...state,
      buttons: buttons.pressed,
      axis: directional.axis,
      heldDirection: repeated.heldDirection,
      repeatAt: repeated.repeatAt,
      centerButtonObservable: state.centerButtonObservable || validButton(active, 16),
    },
    actions: [...buttons.actions, ...repeated.actions].slice(0, 1),
  };
}

export function suspendNavigationController(state: NavigationControllerState) {
  if (state.activeIndex === null) {return state;}
  return {
    ...state,
    phase: "neutral-gate" as const,
    neutralSince: null,
    buttons: Object.freeze([]),
    axis: { direction: null, primary: null },
    heldDirection: null,
    repeatAt: null,
  };
}
