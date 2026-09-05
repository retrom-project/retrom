import {describe, expect, it, vi} from "vitest";

import type {LaunchEnvelopeV1, PlayerRuntimeV1, RuntimeEventV1} from "./contract";
import {mountProviderRuntime} from "./runtime-controller";

describe("provider runtime controller", () => {
  it("creates, mounts, forwards terminal events, and exits exactly once", async () => {
    const runtime = fixtureRuntime();
    const importer = vi.fn(async () => fixtureModule(runtime));
    const onExitRequested = vi.fn();
    const onFatalError = vi.fn();
    const target = document.createElement("div");
    document.body.append(target);

    const controller = await mountProviderRuntime(envelope(), target, {
      dispatcher: verifiedDispatcher(), importer, onExitRequested, onFatalError,
    });
    expect(runtime.mount).toHaveBeenCalledWith(target);
    runtime.emit({type: "FATAL_ERROR", code: "FIXTURE_FATAL"});
    runtime.emit({type: "FATAL_ERROR", code: "SECOND_FATAL"});
    runtime.emit({type: "EXIT_REQUESTED"});
    expect(onExitRequested).not.toHaveBeenCalled();
    expect(onFatalError).toHaveBeenCalledWith("FIXTURE_FATAL");
    expect(onFatalError).toHaveBeenCalledOnce();

    await controller.exit();
    await controller.exit();
    expect(controller.signal.aborted).toBe(true);
    expect(runtime.exit).toHaveBeenCalledOnce();
    expect(runtime.unsubscribe).toHaveBeenCalledOnce();
  });

  it("exits a created runtime when mount fails", async () => {
    const runtime = fixtureRuntime();
    runtime.mount.mockRejectedValueOnce(new Error("mount failed"));
    await expect(mountProviderRuntime(envelope(), document.createElement("div"), {
      dispatcher: verifiedDispatcher(), importer: async () => fixtureModule(runtime),
    })).rejects.toThrow("mount failed");
    expect(runtime.exit).toHaveBeenCalledOnce();
    expect(runtime.unsubscribe).toHaveBeenCalledOnce();
  });

  it("maps an external abort signal onto runtime exit", async () => {
    const abort = new AbortController();
    const runtime = fixtureRuntime();
    await mountProviderRuntime(envelope(), document.createElement("div"), {
      dispatcher: verifiedDispatcher(), importer: async () => fixtureModule(runtime), signal: abort.signal,
    });
    abort.abort();
    await vi.waitFor(() => expect(runtime.exit).toHaveBeenCalledOnce());
  });

  it("keeps Host resources alive until Provider exit cleanup settles", async () => {
    let releaseExit!: () => void;
    const runtime = fixtureRuntime();
    runtime.exit.mockImplementationOnce(() => new Promise<void>((resolve) => {releaseExit = resolve;}));
    const controller = await mountProviderRuntime(envelope(), document.createElement("div"), {
      dispatcher: verifiedDispatcher(), importer: async () => fixtureModule(runtime),
    });

    const exiting = controller.exit();
    await vi.waitFor(() => expect(runtime.exit).toHaveBeenCalledOnce());

    expect(controller.signal.aborted).toBe(false);
    releaseExit();
    await exiting;
    expect(controller.signal.aborted).toBe(true);
  });

  it("subscribes the Host to lifecycle events before mount starts", async () => {
    const runtime = fixtureRuntime();
    const availability = {available: true, reason: null};
    runtime.mount.mockImplementationOnce(async () => {
      runtime.emit({type: "CHECKPOINT_AVAILABILITY_CHANGED", availability});
    });
    const onRuntimeEvent = vi.fn();

    await mountProviderRuntime(envelope(), document.createElement("div"), {
      dispatcher: verifiedDispatcher(), importer: async () => fixtureModule(runtime), onRuntimeEvent,
    });

    expect(onRuntimeEvent).toHaveBeenCalledWith({type: "CHECKPOINT_AVAILABILITY_CHANGED", availability});
  });
});

function fixtureModule(runtime: PlayerRuntimeV1) {
  return {
    createRuntime: vi.fn(async () => runtime), providerApiVersion: 1, providerId: "fixture",
    providerVersion: "1.0.0",
  };
}

function verifiedDispatcher() {
  return {
    createModuleUrl: vi.fn(() => "blob:retrom-provider"), crossOriginIsolated: true,
    fetcher: vi.fn(async () => new Response("module", {
      headers: {"content-length": "6", "content-type": "text/javascript; charset=utf-8"},
    })),
    revokeModuleUrl: vi.fn(), sha256: vi.fn(async () => "a".repeat(64)),
  };
}

function fixtureRuntime() {
  let listener: ((event: RuntimeEventV1) => void) | null = null;
  const unsubscribe = vi.fn(() => {listener = null;});
  return {
    checkpoint: vi.fn(async () => {throw new Error("unused");}),
    closeNativeSettings: vi.fn(async () => {throw new Error("unused");}),
    emit: (event: RuntimeEventV1) => listener?.(event),
    exit: vi.fn(async (): Promise<void> => undefined), getCanvas: () => null,
    getCapabilities: () => envelope().runtime.capabilities,
    getCheckpointAvailability: () => ({available: false, reason: "UNSUPPORTED"}),
    getDiscState: vi.fn(async () => {throw new Error("unused");}),
    getFrameCount: () => null, getState: () => "CREATED" as const,
    getNetplayPort: vi.fn(async () => {throw new Error("unused");}),
    mount: vi.fn(async () => undefined), pause: vi.fn(async () => undefined),
    openNativeSettings: vi.fn(async () => {throw new Error("unused");}),
    resume: vi.fn(async () => undefined), screenshot: vi.fn(async () => new Blob()),
    setInputFilter: vi.fn(async () => {throw new Error("unused");}),
    setVideoMode: vi.fn(async () => {throw new Error("unused");}),
    setVolume: vi.fn(async () => undefined),
    subscribe: vi.fn((next: (event: RuntimeEventV1) => void) => {listener = next; return unsubscribe;}),
    switchDisc: vi.fn(async () => {throw new Error("unused");}),
    unsubscribe,
  } satisfies PlayerRuntimeV1 & {emit(event: RuntimeEventV1): void; unsubscribe: ReturnType<typeof vi.fn>};
}

function envelope(): LaunchEnvelopeV1 {
  const bundle = "b".repeat(64);
  return {
    netplay: null, resources: [], restore: null, schemaVersion: 1,
    runtime: {
      bundleSha256: bundle, capabilities: {checkpoint: false, frameCounter: false, frameMode: "NONE",
        discSwitch: false, inputFilter: false, nativeSettings: false, netplayPort: false, pause: false,
        requiresThreads: false, screenshot: false, standardGamepad: false,
        videoModes: [], volume: false}, checkpoint: null,
      moduleSha256: "a".repeat(64), moduleUrl: `/runtime/providers/fixture/${bundle}/client.mjs`,
      providerApiVersion: 1, providerId: "fixture", providerVersion: "1.0.0",
      runtimeBaseUrl: `/runtime/providers/fixture/${bundle}/`,
      targetId: "fixture",
    },
    session: {coreName: "Fixture Core", id: "018f0f31-26fe-7a31-9d61-4ec92f16d4c3", mode: "SINGLE", platformName: "Fixture",
      purpose: "PRODUCT", returnTo: "/games/fixture", title: "Fixture", warnings: []},
    targetOptions: {},
  };
}
