import type { GamepadSnapshot } from "./input-model";

export type GamepadFrame = Readonly<{
  gamepads: readonly GamepadSnapshot[];
  nowMs: number;
  suspended: boolean;
}>;

export type GamepadFrameListener = (frame: GamepadFrame) => void;

export interface GamepadFrameSource {
  subscribe(listener: GamepadFrameListener): () => void;
}

function snapshot(gamepad: Gamepad): GamepadSnapshot {
  return {
    axes: Array.from(gamepad.axes, (value) => Number.isFinite(value) ? value : Number.NaN),
    buttons: Array.from(gamepad.buttons, (button) => ({ pressed: button.pressed, value: button.value })),
    connected: gamepad.connected,
    index: gamepad.index,
    mapping: gamepad.mapping,
  };
}

export class BrowserGamepadSource implements GamepadFrameSource {
  private listeners = new Set<GamepadFrameListener>();
  private animationFrame: number | null = null;

  subscribe(listener: GamepadFrameListener) {
    this.listeners.add(listener);
    if (this.listeners.size === 1) {
      document.addEventListener("visibilitychange", this.visibilityChanged);
      window.addEventListener("blur", this.windowBlurred);
      window.addEventListener("gamepadconnected", this.connectionChanged);
      window.addEventListener("gamepaddisconnected", this.connectionChanged);
      this.schedule();
    }
    return () => {
      this.listeners.delete(listener);
      if (this.listeners.size === 0) {this.stop();}
    };
  }

  private readonly tick = (nowMs: number) => {
    this.animationFrame = null;
    if (document.visibilityState === "visible" && document.hasFocus()) {
      const gamepads = Array.from(navigator.getGamepads?.() ?? []).filter((value): value is Gamepad => value !== null).map(snapshot);
      this.emit({ gamepads, nowMs, suspended: false });
    }
    this.schedule();
  };

  private readonly visibilityChanged = () => {
    if (document.visibilityState !== "visible") {this.emitSuspended();}
  };

  private readonly windowBlurred = () => this.emitSuspended();
  private readonly connectionChanged = () => this.schedule();

  private emitSuspended() {
    this.emit({ gamepads: [], nowMs: performance.now(), suspended: true });
  }

  private emit(frame: GamepadFrame) {
    for (const listener of this.listeners) {listener(frame);}
  }

  private schedule() {
    if (this.animationFrame === null && this.listeners.size > 0) {
      this.animationFrame = window.requestAnimationFrame(this.tick);
    }
  }

  private stop() {
    if (this.animationFrame !== null) {window.cancelAnimationFrame(this.animationFrame);}
    this.animationFrame = null;
    document.removeEventListener("visibilitychange", this.visibilityChanged);
    window.removeEventListener("blur", this.windowBlurred);
    window.removeEventListener("gamepadconnected", this.connectionChanged);
    window.removeEventListener("gamepaddisconnected", this.connectionChanged);
  }
}

export const browserGamepadSource = new BrowserGamepadSource();
