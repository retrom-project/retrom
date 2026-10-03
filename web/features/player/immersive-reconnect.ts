import { ImmersiveNeutralGate, gamepadButtonPressed, isNeutralGamepads } from "./immersive-controls";

// A released claim arms confirmation once. The subsequent A press must not
// invalidate the neutral phase that made it safe to accept this new input.
export class ImmersiveReconnectConfirmation {
  private readonly neutral = new ImmersiveNeutralGate();
  private armed = false;
  private previousConfirm = false;

  reset() {
    this.neutral.reset();
    this.armed = false;
    this.previousConfirm = false;
  }

  update(gamepads: (Gamepad | null)[], active: Gamepad, nowMs: number) {
    const pressed = gamepadButtonPressed(active, 0);
    if (!this.armed) {
      this.armed = this.neutral.update(isNeutralGamepads(gamepads), nowMs);
      this.previousConfirm = pressed;
      return { ready: this.armed, confirmed: false };
    }
    const confirmed = pressed && !this.previousConfirm;
    this.previousConfirm = pressed;
    return { ready: true, confirmed };
  }
}
