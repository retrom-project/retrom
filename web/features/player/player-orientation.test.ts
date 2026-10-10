import {expect, it, vi} from "vitest";
import {phoneLandscape, OrientationPauseGate} from "./player-orientation";
import {runtimeFixture} from "./player-test-fixture";
import type {RuntimeStateV1} from "./runtime/contract";

it("uses phone identity and device orientation rather than a narrow desktop window or a soft keyboard", () => {
  const browser = (userAgent: string, type?: string, angle?: number, landscape = false) => ({
    navigator: {userAgent}, screen: {orientation: type ? {type} : undefined}, orientation: angle,
    matchMedia: () => ({matches: landscape}),
  }) as unknown as Window;
  expect(phoneLandscape(browser("Mozilla Desktop"))).toBeNull();
  expect(phoneLandscape(browser("iPad", "portrait-primary"))).toBeNull();
  expect(phoneLandscape(browser("Android Mobile", "landscape-primary", 0, false))).toBe(true);
  expect(phoneLandscape(browser("iPhone", undefined, 90, false))).toBe(true);
  expect(phoneLandscape(browser("iPhone", "portrait-primary", 90, true))).toBe(false);
  expect(phoneLandscape(browser("iPhone", undefined, undefined, false))).toBe(false);
});
function runtime(initial: RuntimeStateV1 = "RUNNING") {
  let state = initial;
  const instance = runtimeFixture(initial);
  instance.getState = () => state;
  instance.pause = vi.fn(async () => {state = "PAUSED";});
  instance.resume = vi.fn(async () => {state = "RUNNING";});
  return instance;
}
it("pauses a running game in portrait and restores the same instance only after other overlays/background clear", async () => {
  const instance = runtime(), gate = new OrientationPauseGate(instance, vi.fn());
  await gate.update(true, true);
  expect(instance.getState()).toBe("PAUSED");
  await gate.update(false, false);
  expect(instance.resume).not.toHaveBeenCalled();
  await gate.update(false, true);
  expect(instance.resume).toHaveBeenCalledOnce();
  expect(instance.pause).toHaveBeenCalledOnce();
});
it("never resumes a user-paused game or a session that has been left", async () => {
  const instance = runtime("PAUSED"), gate = new OrientationPauseGate(instance, vi.fn());
  await gate.update(true, true); await gate.update(false, true);
  expect(instance.resume).not.toHaveBeenCalled();
  const running = runtime(), owned = new OrientationPauseGate(running, vi.fn());
  await owned.update(true, true); owned.manualPause(); await owned.update(false, true);
  expect(running.resume).not.toHaveBeenCalled();
  owned.stop(); await owned.update(false, true);
  expect(running.resume).not.toHaveBeenCalled();
});
it("serializes a portrait/landscape change while pause is still in flight", async () => {
  const instance = runtime(), pause = instance.pause;
  let finish = () => {};
  instance.pause = vi.fn(() => new Promise<void>(resolve => {finish = () => {void pause().then(resolve);};}));
  const gate = new OrientationPauseGate(instance, vi.fn());
  const paused = gate.update(true, true);
  await Promise.resolve();
  const resumed = gate.update(false, true);
  finish(); await paused; await resumed;
  expect(instance.resume).toHaveBeenCalledOnce();
  expect(instance.getState()).toBe("RUNNING");
});

it("does not resume after leaving during an unfinished orientation pause", async () => {
  const instance = runtime(), pause = instance.pause;
  let finish = () => {};
  instance.pause = vi.fn(() => new Promise<void>(resolve => {finish = () => {void pause().then(resolve);};}));
  const gate = new OrientationPauseGate(instance, vi.fn());
  const paused = gate.update(true, true); await Promise.resolve();
  gate.stop(); const resumed = gate.update(false, true); finish();
  await paused; await resumed;
  expect(instance.resume).not.toHaveBeenCalled();
});
