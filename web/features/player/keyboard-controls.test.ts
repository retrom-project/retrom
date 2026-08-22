import { describe, expect, it, vi } from "vitest";
import { createRetromDefaultControls, handlePlayerPauseShortcut } from "./keyboard-controls";

const expectedGamepad = {
  0: "BUTTON_2", 1: "BUTTON_4", 2: "SELECT", 3: "START",
  4: "DPAD_UP", 5: "DPAD_DOWN", 6: "DPAD_LEFT", 7: "DPAD_RIGHT",
  8: "BUTTON_1", 9: "BUTTON_3", 10: "LEFT_TOP_SHOULDER", 11: "RIGHT_TOP_SHOULDER",
  12: "LEFT_BOTTOM_SHOULDER", 13: "RIGHT_BOTTOM_SHOULDER", 14: "LEFT_STICK", 15: "RIGHT_STICK",
  16: "LEFT_STICK_X:+1", 17: "LEFT_STICK_X:-1", 18: "LEFT_STICK_Y:+1", 19: "LEFT_STICK_Y:-1",
  20: "RIGHT_STICK_X:+1", 21: "RIGHT_STICK_X:-1", 22: "RIGHT_STICK_Y:+1", 23: "RIGHT_STICK_Y:-1",
};

describe("Retrom keyboard controls", () => {
  it("binds only the requested 1P and 2P keyboard controls", () => {
    const controls = createRetromDefaultControls();
    expect(Object.fromEntries(Object.entries(controls[0]).filter(([, binding]) => binding.value).map(([id, binding]) => [id, binding.value]))).toEqual({
      0: "j", 1: "l", 2: "5", 3: "1", 4: "w", 5: "s", 6: "a", 7: "d", 8: "k", 9: "i",
    });
    expect(Object.fromEntries(Object.entries(controls[1]).filter(([, binding]) => binding.value).map(([id, binding]) => [id, binding.value]))).toEqual({
      0: "numpad 1", 1: "numpad 3", 3: "2", 4: "up arrow", 5: "down arrow",
      6: "left arrow", 7: "right arrow", 8: "numpad 2", 9: "numpad 5",
    });
    expect(Object.values(controls[2]).every((binding) => binding.value === "")).toBe(true);
    expect(Object.values(controls[3]).every((binding) => binding.value === "")).toBe(true);
    expect(Object.values(controls).every((player) => Object.keys(player).length === 30)).toBe(true);
  });

  it("routes the shared coin key to one virtual coin input", () => {
    const controls = createRetromDefaultControls();
    const matches = Object.entries(controls).flatMap(([player, bindings]) => Object.entries(bindings)
      .filter(([, binding]) => binding.value === "5")
      .map(([control]) => ({ player: Number(player), control: Number(control) })));
    expect(matches).toEqual([{ player: 0, control: 2 }]);
    expect(controls[1][2]).toEqual({ value: "" });
  });

  it("preserves every built-in gamepad binding and does not add others", () => {
    const controls = createRetromDefaultControls();
    expect(Object.fromEntries(Object.entries(controls[0]).filter(([, binding]) => binding.value2).map(([id, binding]) => [id, binding.value2]))).toEqual(expectedGamepad);
    expect([1, 2, 3].every((player) => Object.values(controls[player]).every((binding) => binding.value2 === undefined))).toBe(true);
  });

  it("returns a fresh graph because EmulatorJS mutates default controls by platform", () => {
    const first = createRetromDefaultControls();
    const second = createRetromDefaultControls();
    delete first[0][12];
    expect(second[0][12]).toEqual({ value: "", value2: "LEFT_BOTTOM_SHOULDER" });
  });

  it("reserves an unmodified P key for pause without stealing form input", () => {
    const onPause = vi.fn();
    const event = new KeyboardEvent("keydown", { code: "KeyP", cancelable: true });
    expect(handlePlayerPauseShortcut(event, onPause)).toBe(true);
    expect(event.defaultPrevented).toBe(true);
    expect(onPause).toHaveBeenCalledOnce();

    const input = document.createElement("input");
    const inputEvent = new KeyboardEvent("keydown", { code: "KeyP", bubbles: true, cancelable: true });
    input.addEventListener("keydown", (current) => expect(handlePlayerPauseShortcut(current, onPause)).toBe(false));
    input.dispatchEvent(inputEvent);
    expect(onPause).toHaveBeenCalledOnce();
    expect(handlePlayerPauseShortcut(new KeyboardEvent("keydown", { code: "KeyP", repeat: true }), onPause)).toBe(false);
    expect(handlePlayerPauseShortcut(new KeyboardEvent("keydown", { code: "KeyP", ctrlKey: true }), onPause)).toBe(false);
  });
});
