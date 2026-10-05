import {renderHook, cleanup} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import {usePlayerRuntimeActions} from "./player-runtime-actions";
import type {PlayerRuntimeV2} from "./runtime/contract";
import {readVideoRenderingMode} from "./video-rendering";

afterEach(cleanup);

type Params = Parameters<typeof usePlayerRuntimeActions>[0];
function setup(nativeSettings = true) {
  const runtime = {
    getCapabilities: () => ({nativeSettings, videoModes: ["pixel", "smooth"]}),
    openNativeSettings: vi.fn(async () => undefined), closeNativeSettings: vi.fn(async () => undefined),
    setVideoMode: vi.fn(async () => undefined),
  };
  const params: Params = {
    userId: "settings-test", state: "running", runtime: {current: runtime as unknown as PlayerRuntimeV2}, envelope: {current: null},
    manualSaveAvailableRef: {current: true}, programSelectionRequiredRef: {current: false}, uploadManualState: vi.fn(async () => true),
    discState: null, setDiscState: vi.fn(), reportPlayerEvent: vi.fn(), showToast: vi.fn(), setSyncText: vi.fn(), setSyncTone: vi.fn(),
    setEmulatorToolbarOpen: vi.fn(), holdControls: vi.fn(), releaseControls: vi.fn(), lastAudibleVolume: {current: 0.5},
    emulatorVolume: 0.5, emulatorMuted: false, setEmulatorVolume: vi.fn(), setEmulatorMuted: vi.fn(), videoRenderingModeRef: {current: "pixel"},
  };
  const {result} = renderHook(() => usePlayerRuntimeActions(params));
  return {runtime, params, actions: result.current};
}

it("waits for native close before releasing settings ownership and retains it on failure", async () => {
  const {runtime, params, actions} = setup();
  runtime.closeNativeSettings.mockRejectedValueOnce(new Error("native close failed"));
  await expect(actions.closeEmulatorSettings()).resolves.toBe(false);
  expect(params.setEmulatorToolbarOpen).not.toHaveBeenCalled();
  expect(params.releaseControls).not.toHaveBeenCalled();
  expect(params.showToast).toHaveBeenCalledWith("无法关闭运行时设置，请重试。", 3000);
  await expect(actions.closeEmulatorSettings()).resolves.toBe(true);
  expect(params.setEmulatorToolbarOpen).toHaveBeenCalledWith(false);
  expect(params.releaseControls).toHaveBeenCalledOnce();
  expect(runtime.closeNativeSettings.mock.invocationCallOrder[1]).toBeLessThan(vi.mocked(params.releaseControls).mock.invocationCallOrder[0]);
});

it("reports native open failure and returns to host settings without releasing the pause hold", async () => {
  const {runtime, params, actions} = setup();
  runtime.openNativeSettings.mockRejectedValueOnce(new Error("native open failed"));
  await expect(actions.openEmulatorPanel("core")).resolves.toBe(false);
  expect(params.showToast).toHaveBeenCalledWith("无法打开运行时设置，请重试。", 3000);
  await expect(actions.openEmulatorPanel("core")).resolves.toBe(true);
  expect(params.showToast).toHaveBeenLastCalledWith("");
  await expect(actions.openEmulatorPanel(null)).resolves.toBe(true);
  expect(runtime.closeNativeSettings).toHaveBeenCalledOnce();
  expect(params.setEmulatorToolbarOpen).not.toHaveBeenCalled();
  expect(params.releaseControls).not.toHaveBeenCalled();
});

it("can close basic settings on a runtime without native panels", async () => {
  const {runtime, actions} = setup(false);
  await expect(actions.openEmulatorPanel("display")).resolves.toBe(false);
  await expect(actions.closeEmulatorSettings()).resolves.toBe(true);
  expect(runtime.openNativeSettings).not.toHaveBeenCalled();
  expect(runtime.closeNativeSettings).not.toHaveBeenCalled();
});

it("persists only supported picture modes after the runtime accepts the change", async () => {
  window.localStorage.clear();
  const {runtime, params, actions} = setup();
  actions.changeVideoRenderingMode("adaptive-sharpen");
  expect(runtime.setVideoMode).not.toHaveBeenCalled();
  expect(params.videoRenderingModeRef.current).toBe("pixel");
  runtime.setVideoMode.mockRejectedValueOnce(new Error("mode rejected"));
  actions.changeVideoRenderingMode("smooth");
  await vi.waitFor(() => expect(params.showToast).toHaveBeenCalledWith("无法应用画面模式，请重试。", 3000));
  expect(readVideoRenderingMode(params.userId)).toBe("pixel");
  actions.changeVideoRenderingMode("smooth");
  await vi.waitFor(() => expect(readVideoRenderingMode(params.userId)).toBe("smooth"));
  expect(params.videoRenderingModeRef.current).toBe("smooth");
  window.localStorage.clear();
});
