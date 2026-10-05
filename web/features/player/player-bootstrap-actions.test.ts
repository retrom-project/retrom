import {act, renderHook, waitFor} from "@testing-library/react";
import {afterEach, expect, it, vi} from "vitest";
import {initialPlayerOrientationState} from "./orientation";
import {usePlayerBootstrap, type PlayerBootstrapParams} from "./player-bootstrap";
import type {LaunchEnvelopeV1, PlayerRuntimeV2, RuntimeCheckpointAvailabilityV1, RuntimeEventV2} from "./runtime/contract";
import type {RuntimeController} from "./runtime/runtime-controller";

const mounted = vi.hoisted(() => vi.fn());
vi.mock("./runtime/runtime-controller", () => ({mountProviderRuntime: mounted}));
vi.mock("./runtime/envelope", () => ({parseLaunchEnvelopeJSON: JSON.parse}));
afterEach(() => {vi.unstubAllGlobals(); vi.resetAllMocks();});

it("retries a temporary config failure once and never mounts a runtime for the failed attempt", async () => {
  const params = bootstrapParams();
  const fetchMock = vi.fn().mockResolvedValueOnce(new Response(null, {status: 503}))
    .mockResolvedValueOnce(new Response(JSON.stringify({runtime: {capabilities: {videoModes: []}, checkpoint: null},
      session: {purpose: "PRODUCT", returnTo: "/library", warnings: []}})));
  vi.stubGlobal("fetch", fetchMock);
  const runtime = {getCapabilities: () => ({videoModes: []}), getCanvas: () => null,
    getCheckpointAvailability: () => ({available: false, reason: "UNSUPPORTED"})} as unknown as PlayerRuntimeV2;
  mounted.mockResolvedValue({runtime, exit: vi.fn().mockResolvedValue(undefined)});
  const {result, unmount} = renderHook(() => usePlayerBootstrap(params, {current: null}));
  await waitFor(() => expect(params.setState).toHaveBeenCalledWith("error"));
  expect(params.setMessage).toHaveBeenLastCalledWith("PLAYER_LAUNCH_SERVICE_UNAVAILABLE");
  expect(mounted).not.toHaveBeenCalled();
  act(() => {result.current(); result.current();});
  await waitFor(() => expect(params.setState).toHaveBeenLastCalledWith("running"));
  expect(fetchMock).toHaveBeenCalledTimes(2); expect(mounted).toHaveBeenCalledOnce();
  unmount();
});

it("waits for the failed mounted runtime to exit before a retry fetch or mount", async () => {
  const params = bootstrapParams();
  const envelope = {runtime: {capabilities: {videoModes: []}, checkpoint: null}, session: {purpose: "PRODUCT", returnTo: "/library", warnings: []}};
  const fetchMock = vi.fn().mockImplementation(async () => new Response(JSON.stringify(envelope)));
  vi.stubGlobal("fetch", fetchMock);
  let release: () => void = () => undefined;
  const exit = vi.fn(() => new Promise<void>((resolve) => {release = resolve;}));
  const runtime = {getCapabilities: () => {throw new Error("PLAYER_RUNTIME_INITIALIZATION_FAILED");}, getCanvas: () => null} as unknown as PlayerRuntimeV2;
  mounted.mockResolvedValueOnce({runtime, exit}).mockRejectedValueOnce(new Error("second attempt"));
  const {result, unmount} = renderHook(() => usePlayerBootstrap(params, {current: null}));
  await waitFor(() => expect(params.setState).toHaveBeenCalledWith("error"));
  act(() => result.current());
  await waitFor(() => expect(exit).toHaveBeenCalledOnce());
  expect(fetchMock).toHaveBeenCalledOnce(); expect(mounted).toHaveBeenCalledOnce();
  await act(async () => release());
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  expect(mounted).toHaveBeenCalledTimes(2); unmount();
});

it("uses public program-selection actions for an unknown Target and observes action removal", async () => {
  const envelope = {runtime: {targetId: "future-provider-target", capabilities: {videoModes: []},
    checkpoint: {semantics: "INSTANT"}}, targetOptions: {},
    session: {purpose: "PRODUCT", returnTo: "/library", warnings: [], title: "Future game", coreName: "Future", platformName: "Future"},
  } as unknown as LaunchEnvelopeV1;
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(envelope))));
  const availability: RuntimeCheckpointAvailabilityV1 = {available: false, reason: "FUTURE_ACTION", requiredAction: "SELECT_PROGRAM"};
  const runtime = {getCapabilities: () => envelope.runtime.capabilities, getCanvas: () => null,
    getCheckpointAvailability: () => availability} as unknown as PlayerRuntimeV2;
  let emit: ((event: RuntimeEventV2) => void) | undefined;
  mounted.mockImplementation(async (_envelope, _stage, options) => {
    emit = options.onRuntimeEvent;
    return {runtime, exit: vi.fn().mockResolvedValue(undefined)};
  });
  const params = bootstrapParams();
  const {unmount} = renderHook(() => usePlayerBootstrap(params, {current: null}));
  await waitFor(() => expect(params.setState).toHaveBeenCalledWith("running"));
  expect(params.programSelectionRequiredRef.current).toBe(true);
  expect(params.setProgramSelectionRequired).toHaveBeenLastCalledWith(true);
  expect(params.manualSaveAvailableRef.current).toBe(false);
  act(() => emit?.({type: "CHECKPOINT_AVAILABILITY_CHANGED", availability: {available: true, reason: null}}));
  expect(params.programSelectionRequiredRef.current).toBe(false);
  expect(params.setProgramSelectionRequired).toHaveBeenLastCalledWith(false);
  expect(params.manualSaveAvailableRef.current).toBe(true);
  unmount();
});

function bootstrapParams(): PlayerBootstrapParams {
  return {
    launchId: "future-launch", experience: "standard", stage: {current: document.createElement("div")},
    runtime: {current: null}, runtimeController: {current: null as RuntimeController | null}, envelope: {current: null},
    returnTo: {current: "/"}, manualSaveAvailableRef: {current: false}, programSelectionRequiredRef: {current: false},
    orientationStateRef: {current: {...initialPlayerOrientationState}}, videoRenderingModeRef: {current: "pixel"},
    pausedRef: {current: false}, started: {current: false}, finishing: {current: false}, progressTimer: {current: null},
    progressClock: {current: {start: vi.fn(), setPaused: vi.fn(), stop: vi.fn()} as unknown as PlayerBootstrapParams["progressClock"]["current"]},
    toastTimer: {current: null}, setFailure: vi.fn(), setMessage: vi.fn(), setLoadProgress: vi.fn(), setContentLoadingCapability: vi.fn(),
    setState: vi.fn(), setManualSaveAvailable: vi.fn(), setProgramSelectionRequired: vi.fn(), setWarnings: vi.fn(),
    setGameTitle: vi.fn(), setCoreName: vi.fn(), setPlatformName: vi.fn(), setDebugRuntime: vi.fn(), setDiscState: vi.fn(),
    setOrientationState: vi.fn(), setSyncText: vi.fn(), setSyncTone: vi.fn(), setEmulatorVolume: vi.fn(),
    setEmulatorMuted: vi.fn(), setPaused: vi.fn(), setReviewScreenshotAvailable: vi.fn(), setPlayerReturnTo: vi.fn(),
    reportPlayerEvent: vi.fn(), onKeyboardPause: vi.fn(), onImmersiveMenuShortcut: vi.fn(), onRevealControls: vi.fn(),
    onShowControls: vi.fn(), onGameSurface: vi.fn(), onExitRequested: vi.fn(), reportProgress: vi.fn().mockResolvedValue(undefined),
  };
}
