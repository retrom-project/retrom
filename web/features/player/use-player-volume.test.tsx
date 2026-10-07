import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { cleanup } from "@testing-library/react";
import { getImmersiveAudioPreferences, saveImmersiveAudioPreferences, DEFAULT_IMMERSIVE_AUDIO_PREFERENCES } from "@/features/immersive/immersive-audio-preferences";
import { runtimeFixture } from "./player-test-fixture";
import type { RuntimeStateV1 } from "./runtime/contract";
import { usePlayerVolume } from "./use-player-volume";

beforeEach(() => localStorage.clear());
afterEach(cleanup);
it.each([false, true])("applies the saved immersive game volume after mount, including muted=%s", async (muted) => {
  saveImmersiveAudioPreferences({ ...DEFAULT_IMMERSIVE_AUDIO_PREFERENCES, gameVolume: 0.3, gameMuted: muted });
  const instance = runtimeFixture();
  const capabilities = runtimeFixture().getCapabilities();
  instance.getCapabilities = () => ({ ...capabilities, volume: true });
  instance.setVolume = vi.fn(async () => undefined);
  const runtime = { current: instance };
  const props = { state: "MOUNTING" as RuntimeStateV1 };
  const hook = renderHook(({ state }) => usePlayerVolume(runtime, state, true, vi.fn()), { initialProps: props });
  expect(instance.setVolume).not.toHaveBeenCalled();
  hook.rerender({ state: "RUNNING" });
  await waitFor(() => expect(instance.setVolume).toHaveBeenCalledWith(muted ? 0 : 0.3));
  act(() => hook.result.current.change(0.7));
  await waitFor(() => expect(instance.setVolume).toHaveBeenLastCalledWith(0.7));
  expect(getImmersiveAudioPreferences()).toMatchObject({ gameVolume: 0.7, gameMuted: false });
  hook.rerender({ state: "PAUSED" });
  expect(instance.setVolume).toHaveBeenCalledTimes(2);
});
it("reports an unsupported runtime operation without claiming it succeeded", async () => {
  const instance = runtimeFixture();
  const capabilities = instance.getCapabilities();
  instance.getCapabilities = () => ({ ...capabilities, volume: true });
  instance.setVolume = vi.fn().mockRejectedValue(new Error("音量设置失败"));
  const onError = vi.fn();
  renderHook(() => usePlayerVolume({ current: instance }, "RUNNING", false, onError));
  await waitFor(() => expect(onError).toHaveBeenCalledWith("音量设置失败"));
});
