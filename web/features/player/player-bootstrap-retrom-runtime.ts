import type { EmulatorInstance, PlayerConfig } from "./adapters/ejs-4.2.3-v2";
import type { GameRuntime } from "@xxxsen/retrom-runtime";
import {
  butterscotchPlayerInstance,
  createButterscotchProductRuntime,
  type ButterscotchLaunchConfig,
} from "./butterscotch-runtime";
import {
  butterscotchShellConfig,
  fetchButterscotchCheckpoint,
  fetchKiriKiriCheckpoint,
  fetchOnsCheckpoint,
  kirikiriShellConfig,
  onsShellConfig,
  fetchTyranoScriptCheckpoint,
  tyranoScriptShellConfig,
  fetchWASM4Checkpoint,
  wasm4ShellConfig,
} from "./player-bootstrap-config";
import type {
  BootstrapResources,
  MountedContext,
  PlayerBootstrapParams,
} from "./player-bootstrap";
import { handleRetromRuntimeEvent } from "./player-bootstrap-ons";
import {
  isKiriKiriLaunchConfig,
  mountKiriKiriProductRuntime,
  type KiriKiriLaunchConfig,
} from "./kirikiri-runtime";
import {
  createOnsProductRuntime,
  isOnsLaunchConfig,
  onsPlayerInstance,
  type OnsLaunchConfig,
} from "./ons-runtime";
import { installRuntimeImmersiveGamepadFilter } from "./runtime-immersive-gamepad";
import {
  createTyranoScriptProductRuntime,
  isTyranoScriptLaunchConfig,
  tyranoScriptPlayerInstance,
  type TyranoScriptLaunchConfig,
} from "./tyranoscript-runtime";
import {
  createWASM4ProductRuntime,
  isWASM4LaunchConfig,
  wasm4PlayerInstance,
  type WASM4LaunchConfig,
} from "./wasm4-runtime";

type MountedFrame = { target: HTMLElement; context: MountedContext };

export type RetromRuntimeBootstrapHost = {
  applyConfig: (params: PlayerBootstrapParams, config: PlayerConfig) => void;
  prepareOrientation: (
    params: PlayerBootstrapParams,
    config: PlayerConfig,
    controller: AbortController,
  ) => Promise<void>;
  prepareFrame: (
    params: PlayerBootstrapParams,
    controller: AbortController,
  ) => Promise<HTMLIFrameElement>;
  mountFrame: (
    params: PlayerBootstrapParams,
    resources: BootstrapResources,
    controller: AbortController,
    config: PlayerConfig,
    frame: HTMLIFrameElement,
    stateBytes: Uint8Array | null,
    crossOriginFrame?: boolean,
  ) => MountedFrame;
  handleReady: (context: MountedContext, instance: EmulatorInstance) => void;
  completeStart: (context: MountedContext, resumeMainLoop: boolean) => void;
};

export { isKiriKiriLaunchConfig, isOnsLaunchConfig };
export { isTyranoScriptLaunchConfig };
export { isWASM4LaunchConfig };

export async function bootstrapOnsPlayer(
  params: PlayerBootstrapParams,
  resources: BootstrapResources,
  controller: AbortController,
  runtimeConfig: OnsLaunchConfig,
  host: RetromRuntimeBootstrapHost,
) {
  const config = onsShellConfig(runtimeConfig);
  host.applyConfig(params, config);
  setDebugRuntime(params, runtimeConfig, "ONS");
  const mounted = await prepareRuntimeFrame(params, resources, controller, config, host,
    fetchOnsCheckpoint(runtimeConfig, controller.signal));
  params.setMessage("正在启动 ONScripter 运行时…");
  const runtime = createOnsProductRuntime(runtimeConfig, mounted.context.frameWindow,
    mounted.context.stateBytes, controller.signal);
  resources.nativeRuntimeSubscription = runtime.subscribe((event) => handleRetromRuntimeEvent(event, params));
  await mountRuntime(params, resources, controller, mounted, runtime,
    () => onsPlayerInstance(runtime, mounted.target), host);
}

export async function bootstrapKiriKiriPlayer(
  params: PlayerBootstrapParams,
  resources: BootstrapResources,
  controller: AbortController,
  runtimeConfig: KiriKiriLaunchConfig,
  host: RetromRuntimeBootstrapHost,
) {
  const config = kirikiriShellConfig(runtimeConfig);
  host.applyConfig(params, config);
  setDebugRuntime(params, runtimeConfig, "KIRIKIRI");
  const mounted = await prepareRuntimeFrame(params, resources, controller, config, host,
    fetchKiriKiriCheckpoint(runtimeConfig, controller.signal));
  params.setMessage("正在启动 KiriKiri 运行时…");
  const mountedRuntime = await mountKiriKiriProductRuntime(
    runtimeConfig, mounted.target, mounted.context.frameWindow, mounted.context.stateBytes, controller.signal,
  );
  resources.nativeRuntimeSubscription = mountedRuntime.runtime.subscribe((event) => {
    handleRetromRuntimeEvent(event, params);
  });
  await finishMountedRuntime(params, resources, controller, mounted, mountedRuntime.runtime,
    mountedRuntime.instance, host);
}

export async function bootstrapButterscotchPlayer(
  params: PlayerBootstrapParams,
  resources: BootstrapResources,
  controller: AbortController,
  runtimeConfig: ButterscotchLaunchConfig,
  host: RetromRuntimeBootstrapHost,
) {
  const config = butterscotchShellConfig(runtimeConfig);
  host.applyConfig(params, config);
  setDebugRuntime(params, runtimeConfig, "BUTTERSCOTCH");
  const mounted = await prepareRuntimeFrame(params, resources, controller, config, host,
    fetchButterscotchCheckpoint(runtimeConfig, controller.signal));
  params.setMessage("正在启动 GameMaker 运行时…");
  const runtime = createButterscotchProductRuntime(
    runtimeConfig, mounted.context.frameWindow, mounted.context.stateBytes, controller.signal,
  );
  resources.nativeRuntimeSubscription = runtime.subscribe((event) => handleRetromRuntimeEvent(event, params));
  await mountRuntime(params, resources, controller, mounted, runtime,
    () => butterscotchPlayerInstance(runtime, mounted.target), host);
}

export async function bootstrapTyranoScriptPlayer(
  params: PlayerBootstrapParams,
  resources: BootstrapResources,
  controller: AbortController,
  runtimeConfig: TyranoScriptLaunchConfig,
  host: RetromRuntimeBootstrapHost,
) {
  const config = tyranoScriptShellConfig(runtimeConfig);
  host.applyConfig(params, config);
  setDebugRuntime(params, runtimeConfig, "TYRANOSCRIPT");
  const mounted = await prepareRuntimeFrame(params, resources, controller, config, host,
    fetchTyranoScriptCheckpoint(runtimeConfig, controller.signal), true);
  params.setMessage("正在启动 TyranoScript 运行时…");
  const runtime = createTyranoScriptProductRuntime(
    runtimeConfig, mounted.context.frame, mounted.context.frameWindow,
    mounted.context.stateBytes, controller.signal,
  );
  resources.nativeRuntimeSubscription = runtime.subscribe((event) => handleRetromRuntimeEvent(event, params));
  await mountRuntime(params, resources, controller, mounted, runtime,
    () => tyranoScriptPlayerInstance(runtime, mounted.target), host);
}

export async function bootstrapWASM4Player(
  params: PlayerBootstrapParams,
  resources: BootstrapResources,
  controller: AbortController,
  runtimeConfig: WASM4LaunchConfig,
  host: RetromRuntimeBootstrapHost,
) {
  const config = wasm4ShellConfig(runtimeConfig);
  host.applyConfig(params, config);
  setDebugRuntime(params, runtimeConfig, "WASM4");
  const mounted = await prepareRuntimeFrame(params, resources, controller, config, host,
    fetchWASM4Checkpoint(runtimeConfig, controller.signal));
  params.setMessage("正在启动 WASM-4 运行时…");
  const runtime = createWASM4ProductRuntime(
    runtimeConfig, mounted.context.frameWindow, mounted.context.stateBytes, controller.signal,
  );
  resources.nativeRuntimeSubscription = runtime.subscribe((event) => handleRetromRuntimeEvent(event, params));
  await mountRuntime(params, resources, controller, mounted, runtime,
    () => wasm4PlayerInstance(runtime, mounted.target), host);
}

type RuntimeIdentity = {
  coreId: string;
  artifactId: string;
  runtimeVersion: string;
  adapter: { adapterId: string };
};

function setDebugRuntime(
  params: PlayerBootstrapParams,
  config: RuntimeIdentity,
  runtimeFamily: "ONS" | "KIRIKIRI" | "BUTTERSCOTCH" | "TYRANOSCRIPT" | "WASM4",
) {
  params.setDebugRuntime({
    runtimeFamily, coreId: config.coreId, coreArtifactId: config.artifactId,
    emulatorJSVersion: config.runtimeVersion, playerAdapterId: config.adapter.adapterId,
    inputMode: "STANDARD", crossOriginIsolated: window.crossOriginIsolated,
    sharedArrayBuffer: typeof SharedArrayBuffer !== "undefined",
  });
}

async function prepareRuntimeFrame(
  params: PlayerBootstrapParams,
  resources: BootstrapResources,
  controller: AbortController,
  config: PlayerConfig,
  host: RetromRuntimeBootstrapHost,
  statePromise: Promise<Uint8Array | null>,
  crossOriginFrame = false,
) {
  await host.prepareOrientation(params, config, controller);
  const frame = await host.prepareFrame(params, controller);
  const stateBytes = await statePromise;
  const mounted = host.mountFrame(
    params, resources, controller, config, frame, stateBytes, crossOriginFrame,
  );
  resources.cleanupRuntimeGamepadFilter = installRuntimeImmersiveGamepadFilter(
    params.experience, mounted.context.frameWindow, params.immersiveGamepadFilter,
  );
  return mounted;
}

async function mountRuntime(
  params: PlayerBootstrapParams,
  resources: BootstrapResources,
  controller: AbortController,
  mounted: MountedFrame,
  runtime: GameRuntime,
  instance: () => EmulatorInstance,
  host: RetromRuntimeBootstrapHost,
) {
  try {
    await runtime.mount(mounted.target);
    await finishMountedRuntime(params, resources, controller, mounted, runtime, instance(), host);
  } catch (error) {
    await runtime.exit();
    throw error;
  }
}

async function finishMountedRuntime(
  params: PlayerBootstrapParams,
  resources: BootstrapResources,
  controller: AbortController,
  mounted: MountedFrame,
  runtime: GameRuntime,
  instance: EmulatorInstance,
  host: RetromRuntimeBootstrapHost,
) {
  if (controller.signal.aborted) {await runtime.exit(); return;}
  resources.cleanup = () => runtime.exit();
  host.handleReady(mounted.context, instance);
  const availability = runtime.getCheckpointAvailability();
  params.manualSaveAvailableRef.current = availability.available;
  params.setManualSaveAvailable(availability.available);
  host.completeStart(mounted.context, false);
}
