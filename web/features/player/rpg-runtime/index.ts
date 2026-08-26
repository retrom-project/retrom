import { mountEasyRpg } from "./easyrpg/adapter";
import { mountMkxp } from "./mkxp/adapter";
import { mountNativeRpg } from "./native-web/adapter";
import { RetromRpgRuntimeController } from "./controller";
import type { RetromRpgRuntime, RpgRuntimeConfig } from "./contract";
import { adaptMountedRpgAdapter, type RpgPlayerInstance } from "./internal-adapter";
import { isRpgRuntimeConfig, validateRpgRuntimeConfig } from "./registry";

export type {
  CheckpointAvailability,
  CheckpointPayload,
  CheckpointPayloadKind,
  RetromRpgRuntime,
  RpgCoreId,
  RpgGeneration,
  RpgPosition,
  RpgRuntimeConfig,
  RuntimeEvent,
  RuntimeState,
} from "./contract";
export { isRpgRuntimeConfig, rpgRuntimeRoutes, validateRpgRuntimeConfig } from "./registry";

export type RetromRpgRuntimeOptions = {
  frame: HTMLIFrameElement;
  frameWindow: Window;
  restorePayload: Uint8Array | null;
  signal?: AbortSignal;
};

export type RetromRpgRuntimeDescription = {
  crossOriginFrame: boolean;
  requiresThreads: boolean;
  runtimeBaseUrl: string;
};

export function describeRetromRpgRuntime(config: RpgRuntimeConfig): RetromRpgRuntimeDescription {
  validateRpgRuntimeConfig(config);
  if (config.adapter.adapterKind === "NATIVE_WEB") {
    return { crossOriginFrame: true, requiresThreads: false, runtimeBaseUrl: config.adapter.uniqueOrigin };
  }
  return {
    crossOriginFrame: false,
    requiresThreads: config.adapter.adapterKind === "MKXP_LIBRETRO_WEB",
    runtimeBaseUrl: config.adapter.runtimeBaseUrl,
  };
}

export function createRetromRpgRuntime(
  config: RpgRuntimeConfig,
  options: RetromRpgRuntimeOptions,
): RetromRpgRuntime {
  validateRpgRuntimeConfig(config);
  return createController(config, options);
}

export async function mountRetromRpgRuntime(
  config: RpgRuntimeConfig,
  target: HTMLElement,
  options: RetromRpgRuntimeOptions,
): Promise<{ runtime: RetromRpgRuntime; playerInstance: RpgPlayerInstance }> {
  validateRpgRuntimeConfig(config);
  const controller = createController(config, options);
  await controller.mount(target);
  return { runtime: controller, playerInstance: controller.getPlayerInstance() };
}

function createController(config: RpgRuntimeConfig, options: RetromRpgRuntimeOptions) {
  const mountAdapter = adapterMount(config, options);
  return new RetromRpgRuntimeController(
    async (target) => {
      const mounted = await mountAdapter(target);
      try {return adaptMountedRpgAdapter(mounted);}
      catch (error) {await mounted.cleanup(); throw error;}
    },
    options.signal ?? null,
    config.purpose === "RPG_RUNTIME_VALIDATION",
  );
}

function adapterMount(config: RpgRuntimeConfig, options: RetromRpgRuntimeOptions) {
  switch (config.adapter.adapterKind) {
  case "EASYRPG_WEB":
    return (target: HTMLElement) => mountEasyRpg(narrowEasy(config), target, options.frameWindow, options.restorePayload);
  case "MKXP_LIBRETRO_WEB":
    return (target: HTMLElement) => mountMkxp(narrowMkxp(config), target, options.restorePayload);
  case "NATIVE_WEB":
    return () => mountNativeRpg(narrowNative(config), options.frame, options.restorePayload);
  }
}

function narrowEasy(config: RpgRuntimeConfig) {
  return config as RpgRuntimeConfig & {
    adapter: Extract<RpgRuntimeConfig["adapter"], { adapterKind: "EASYRPG_WEB" }>;
  };
}

function narrowMkxp(config: RpgRuntimeConfig) {
  return config as RpgRuntimeConfig & {
    adapter: Extract<RpgRuntimeConfig["adapter"], { adapterKind: "MKXP_LIBRETRO_WEB" }>;
  };
}

function narrowNative(config: RpgRuntimeConfig) {
  return config as RpgRuntimeConfig & {
    adapter: Extract<RpgRuntimeConfig["adapter"], { adapterKind: "NATIVE_WEB" }>;
  };
}

export function isRetromRpgRuntimeConfig(value: unknown): value is RpgRuntimeConfig {
  return isRpgRuntimeConfig(value);
}
