import {act, cleanup, render, screen} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import {PlayerInputDebug, describeInput, heldInputs} from "./player-input-debug";
import type {PlayerRuntimeV1, RuntimeInputDiagnosticsSnapshotV1} from "./runtime/contract";

afterEach(() => {cleanup(); vi.useRealTimers();});

const empty: RuntimeInputDiagnosticsSnapshotV1 = {events: [], held: [], dropped: 0, focus: "GAME", keyboard: true, gamepad: true, delivery: true, coreRead: false};

it("samples only the mounted diagnostics component, leaves pause/resume alone and stops on unmount", () => {
  vi.useFakeTimers();
  const stop = vi.fn(); const read = vi.fn(() => empty);
  const pause = vi.fn(); const resume = vi.fn();
  const start = vi.fn(() => ({stop, read, clear: vi.fn()}));
  const runtimeRef = {current: {startInputDiagnostics: start, pause, resume} as unknown as PlayerRuntimeV1};
  const {unmount} = render(<PlayerInputDebug ready coreName="fixture" runtimeRef={runtimeRef} />);
  act(() => {vi.advanceTimersByTime(1000);});
  expect(start).toHaveBeenCalledOnce(); expect(read).toHaveBeenCalledTimes(10);
  expect(screen.getByText("游戏画布")).toBeVisible();
  expect(pause).not.toHaveBeenCalled(); expect(resume).not.toHaveBeenCalled();
  unmount(); expect(stop).toHaveBeenCalledOnce();
  act(() => {vi.advanceTimersByTime(1000);});
  expect(read).toHaveBeenCalledTimes(10);
});

it("does not enable observers before mount and clearly supports older providers", () => {
  const start = vi.fn();
  const runtimeRef = {current: {startInputDiagnostics: start} as unknown as PlayerRuntimeV1};
  render(<PlayerInputDebug ready={false} coreName="fixture" runtimeRef={runtimeRef} />);
  expect(start).not.toHaveBeenCalled();
  expect(screen.getAllByText("未接入")).toHaveLength(3);
});

it("keeps delivery and game behavior distinct and renders release durations", () => {
  const text = describeInput({sequence: 1, atMs: 100, device: "gamepad:0", control: "Button 0", value: 0,
    stage: "DELIVERED", target: "MouseLeft", reason: null, heldMs: 125.5});
  expect(text).toContain("松开 → MouseLeft · 已投递 · 126 ms");
  expect(text).not.toContain("核心已读取");
});

it("shows one held entry per physical input across observation stages", () => {
  const press = {atMs: 100, value: 1, target: null, reason: null, heldMs: null} as const;
  const held = heldInputs([
    {...press, sequence: 1, device: "gamepad:0", control: "Button 0", stage: "BROWSER"},
    {...press, sequence: 2, device: "gamepad:0", control: "Button 0", stage: "RUNTIME"},
    {...press, sequence: 3, device: "player:0", control: "Control 8", stage: "DELIVERED", target: "core-input"},
  ]);
  expect(held.map((event) => `${event.device}/${event.control}/${event.stage}`))
    .toEqual(["gamepad:0/Button 0/RUNTIME", "player:0/Control 8/DELIVERED"]);
});

