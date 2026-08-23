import { normalizeGamepad } from "@/features/gamepad/browser-source";
import {
  initialNavigationControllerState,
  isControllerNeutral,
  isStandardNavigationController,
  updateNavigationController,
  type NavigationControllerState,
} from "@/features/gamepad/model";
import type {
  ControllerAction,
  ControllerSnapshot,
} from "@/features/gamepad/types";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";

const COMBINATION_WINDOW_MS = 100;
const TAP_PULSE_MS = 50;
const LONG_PRESS_MS = 1_200;
const CONTROL_COUNT = 24;

type CombinationPhase = "idle" | "candidate" | "passthrough" | "active";
type InputOwner = "gameplay" | "overlay" | "closing";

export type PlayerControllerAction =
  | ControllerAction
  | Readonly<{ type: "open-menu" }>
  | Readonly<{ type: "open-exit-confirmation" }>
  | Readonly<{ type: "gameplay-ready" }>;

export type PlayerGamepadHostBridge = Readonly<{
  filterGamepads: (gamepads: readonly (Gamepad | null)[], now: number) => readonly (Gamepad | null)[];
}>;

function pressed(snapshot: ControllerSnapshot, index: number) {
  const button = snapshot.buttons[index];
  return Boolean(button && (button.pressed || button.value >= 0.5));
}

function hasInput(snapshot: ControllerSnapshot) {
  return snapshot.buttons.some((button) => button.pressed || button.value >= 0.5) ||
    snapshot.axes.slice(0, 2).some((axis) => Math.abs(axis) >= 0.6);
}

function zeroButton(): GamepadButton {
  return Object.freeze({ pressed: false, touched: false, value: 0 });
}

function cloneGamepad(
  gamepad: Gamepad,
  buttons: readonly GamepadButton[],
  axes: readonly number[],
) {
  return Object.freeze({
    id: gamepad.id,
    index: gamepad.index,
    connected: gamepad.connected,
    mapping: gamepad.mapping,
    timestamp: gamepad.timestamp,
    buttons: Object.freeze(Array.from(buttons)),
    axes: Object.freeze(Array.from(axes)),
    vibrationActuator: gamepad.vibrationActuator,
  }) as Gamepad;
}

export class PlayerGamepadHostControls implements PlayerGamepadHostBridge {
  private activeIndex: number | null;
  private owner: InputOwner = "gameplay";
  private actions: PlayerControllerAction[] = [];
  private combinationPhase: CombinationPhase = "idle";
  private candidateButton: 8 | 9 | null = null;
  private candidateAt = 0;
  private combinationAt = 0;
  private combinationLongTriggered = false;
  private pulseButton: 8 | 9 | null = null;
  private pulseUntil = 0;
  private centerPressed = false;
  private centerAt = 0;
  private centerLongTriggered = false;
  private overlayNavigation: NavigationControllerState;
  private disconnected = false;

  constructor(activeIndex: number | null = null) {
    this.activeIndex = activeIndex;
    this.overlayNavigation = initialNavigationControllerState(activeIndex);
  }

  currentIndex() {
    return this.activeIndex;
  }

  inputOwner() {
    return this.owner;
  }

  setActiveIndex(index: number | null) {
    this.activeIndex = index;
    this.overlayNavigation = initialNavigationControllerState(index);
    this.disconnected = false;
  }

  openOverlay() {
    if (this.owner === "overlay") {return;}
    this.owner = "overlay";
    this.overlayNavigation = initialNavigationControllerState(this.activeIndex);
  }

  requestGameplay() {
    if (this.owner === "gameplay") {return;}
    this.owner = "closing";
    this.overlayNavigation = initialNavigationControllerState(this.activeIndex);
  }

  suspendUntilNeutral() {
    if (this.owner !== "gameplay") {return;}
    this.owner = "closing";
    this.overlayNavigation = initialNavigationControllerState(this.activeIndex);
    this.combinationPhase = "idle";
    this.candidateButton = null;
    this.pulseButton = null;
    this.centerPressed = false;
  }

  drainActions() {
    const actions = this.actions;
    this.actions = [];
    return actions;
  }

  sample(snapshots: readonly (ControllerSnapshot | null)[], now: number) {
    const active = this.resolveActive(snapshots);
    if (!active) {return;}
    if (this.owner === "gameplay") {this.sampleGameplay(active, now);}
    else {this.sampleOverlay(snapshots, now);}
  }

  filterGamepads(gamepads: readonly (Gamepad | null)[], now: number) {
    const snapshots = gamepads.map((gamepad) => gamepad ? normalizeGamepad(gamepad) : null);
    this.sample(snapshots, now);
    return Object.freeze(gamepads.map((gamepad) => gamepad ? this.filteredGamepad(gamepad, now) : null));
  }

  private resolveActive(snapshots: readonly (ControllerSnapshot | null)[]) {
    if (this.activeIndex !== null) {
      const active = snapshots.find((snapshot) => snapshot?.index === this.activeIndex) ?? null;
      if (active?.connected && isStandardNavigationController(active)) {this.disconnected = false; return active;}
      if (!this.disconnected) {
        this.actions.push({ type: "disconnected", index: this.activeIndex });
        this.disconnected = true;
      }
      this.activeIndex = null;
      this.owner = "overlay";
      this.overlayNavigation = initialNavigationControllerState();
    }
    const claim = snapshots.find((snapshot): snapshot is ControllerSnapshot =>
      Boolean(snapshot && isStandardNavigationController(snapshot) && hasInput(snapshot))) ?? null;
    if (!claim) {return null;}
    this.activeIndex = claim.index;
    this.disconnected = false;
    this.overlayNavigation = initialNavigationControllerState(claim.index);
    this.actions.push({
      type: "claimed",
      index: claim.index,
      centerButtonObservable: claim.buttons.length > 16,
    });
    return claim;
  }

  private sampleGameplay(active: ControllerSnapshot, now: number) {
    this.sampleCenter(active, now);
    this.sampleCombination(active, now);
  }

  private sampleCenter(active: ControllerSnapshot, now: number) {
    const current = pressed(active, 16);
    if (current && !this.centerPressed) {
      this.centerAt = now;
      this.centerLongTriggered = false;
      this.actions.push({ type: "open-menu" });
    }
    if (current && !this.centerLongTriggered && now - this.centerAt >= LONG_PRESS_MS) {
      this.centerLongTriggered = true;
      this.actions.push({ type: "open-exit-confirmation" });
    }
    if (!current) {this.centerLongTriggered = false;}
    this.centerPressed = current;
  }

  private sampleCombination(active: ControllerSnapshot, now: number) {
    const select = pressed(active, 8);
    const start = pressed(active, 9);
    if (this.releaseCombinationCandidate(select, start, now)) {return;}
    if (!select && !start) {
      this.resetCombination();
      return;
    }
    if (this.combinationPhase === "idle") {this.beginCombinationCandidate(select, start, now);}
    else if (this.combinationPhase === "candidate") {this.updateCombinationCandidate(select, start, now);}
    else if (this.combinationPhase === "active") {this.updateActiveCombination(now);}
  }

  private releaseCombinationCandidate(select: boolean, start: boolean, now: number) {
    if (this.combinationPhase !== "candidate" || select || start ||
      now - this.candidateAt > COMBINATION_WINDOW_MS) {return false;}
    this.pulseButton = this.candidateButton;
    this.pulseUntil = now + TAP_PULSE_MS;
    this.combinationPhase = "passthrough";
    return true;
  }

  private resetCombination() {
    this.combinationPhase = "idle";
    this.candidateButton = null;
    this.combinationLongTriggered = false;
  }

  private beginCombinationCandidate(select: boolean, start: boolean, now: number) {
    if (select && start) {this.activateCombination(now); return;}
    this.combinationPhase = "candidate";
    this.candidateButton = select ? 8 : 9;
    this.candidateAt = now;
  }

  private updateCombinationCandidate(select: boolean, start: boolean, now: number) {
    const candidateHeld = this.candidateButton === 8 ? select : start;
    if (candidateHeld && select && start && now - this.candidateAt <= COMBINATION_WINDOW_MS) {
      this.activateCombination(now);
    } else if (!candidateHeld && now - this.candidateAt <= COMBINATION_WINDOW_MS) {
      this.pulseButton = this.candidateButton;
      this.pulseUntil = now + TAP_PULSE_MS;
      this.combinationPhase = "passthrough";
    } else if (now - this.candidateAt > COMBINATION_WINDOW_MS) {
      this.combinationPhase = "passthrough";
    }
  }

  private updateActiveCombination(now: number) {
    if (!this.combinationLongTriggered && now - this.combinationAt >= LONG_PRESS_MS) {
      this.combinationLongTriggered = true;
      this.actions.push({ type: "open-exit-confirmation" });
    }
  }

  private activateCombination(now: number) {
    this.combinationPhase = "active";
    this.combinationAt = now;
    this.combinationLongTriggered = false;
    this.pulseButton = null;
    this.actions.push({ type: "open-menu" });
  }

  private sampleOverlay(snapshots: readonly (ControllerSnapshot | null)[], now: number) {
    const active = snapshots.find((snapshot) => snapshot?.index === this.activeIndex) ?? null;
    if (active) {this.sampleHeldSystemShortcut(active, now);}
    const result = updateNavigationController(this.overlayNavigation, snapshots, now);
    this.overlayNavigation = result.state;
    for (const action of result.actions) {
      if (action.type === "claimed") {
        this.activeIndex = action.index;
        this.actions.push(action);
      } else if (action.type === "ready" && this.owner === "closing") {
        this.owner = "gameplay";
        this.actions.push({ type: "gameplay-ready" });
      } else if (action.type !== "navigation" && this.owner === "overlay") {
        this.actions.push(action);
      }
    }
  }

  private sampleHeldSystemShortcut(active: ControllerSnapshot, now: number) {
    if (this.centerPressed) {
      const current = pressed(active, 16);
      if (current && !this.centerLongTriggered && now - this.centerAt >= LONG_PRESS_MS) {
        this.centerLongTriggered = true;
        this.actions.push({ type: "open-exit-confirmation" });
      }
      if (!current) {this.centerPressed = false; this.centerLongTriggered = false;}
    }
    if (this.combinationPhase !== "active") {return;}
    const bothPressed = pressed(active, 8) && pressed(active, 9);
    if (bothPressed && !this.combinationLongTriggered && now - this.combinationAt >= LONG_PRESS_MS) {
      this.combinationLongTriggered = true;
      this.actions.push({ type: "open-exit-confirmation" });
    }
    if (!pressed(active, 8) && !pressed(active, 9)) {
      this.combinationPhase = "idle";
      this.combinationLongTriggered = false;
    }
  }

  private filteredGamepad(gamepad: Gamepad, now: number) {
    if (this.owner !== "gameplay") {
      return cloneGamepad(gamepad, gamepad.buttons.map(zeroButton), gamepad.axes.map(() => 0));
    }
    if (gamepad.index !== this.activeIndex) {return gamepad;}
    const buttons = gamepad.buttons.map((button) => Object.freeze({
      pressed: button.pressed,
      touched: button.touched,
      value: button.value,
    }));
    if (buttons[16]) {buttons[16] = zeroButton();}
    if (this.combinationPhase === "candidate" && this.candidateButton !== null && buttons[this.candidateButton]) {
      buttons[this.candidateButton] = zeroButton();
    }
    if (this.combinationPhase === "active") {
      if (buttons[8]) {buttons[8] = zeroButton();}
      if (buttons[9]) {buttons[9] = zeroButton();}
    }
    if (this.pulseButton !== null) {
      if (now < this.pulseUntil && buttons[this.pulseButton]) {
        buttons[this.pulseButton] = Object.freeze({ pressed: true, touched: true, value: 1 });
      } else if (now >= this.pulseUntil) {
        this.pulseButton = null;
      }
    }
    return cloneGamepad(gamepad, buttons, gamepad.axes);
  }
}

export function releaseAllPlayerInputs(instance: EmulatorInstance | undefined) {
  const simulate = instance?.gameManager?.simulateInput?.bind(instance.gameManager);
  if (!simulate) {return false;}
  for (let player = 0; player < 4; player += 1) {
    for (let control = 0; control < CONTROL_COUNT; control += 1) {simulate(player, control, 0);}
  }
  return true;
}

export function controllerIsNeutral(
  snapshots: readonly (ControllerSnapshot | null)[],
  activeIndex: number | null,
) {
  const active = snapshots.find((snapshot) => snapshot?.index === activeIndex) ?? null;
  return Boolean(active && isControllerNeutral(active));
}
