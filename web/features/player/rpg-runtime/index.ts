import {
  createRuntime,
  describeRuntime,
  mountRuntime,
  type RpgMakerPositionV1 as RuntimePosition,
  type RpgMakerRuntimeConfig as LibraryRuntimeConfig,
  type RuntimeOptions,
} from "@xxxsen/retrom-runtime";

import { retromRuntimePlayerInstance } from "../retrom-runtime-player";
import type { RpgPosition, RpgRuntimeConfig } from "./contract";
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

export const isRetromRpgRuntimeConfig = isRpgRuntimeConfig;

export type RetromRpgRuntimeOptions = Omit<RuntimeOptions, "onDiagnostic">;

export function describeRetromRpgRuntime(config: RpgRuntimeConfig) {
  validateRpgRuntimeConfig(config);
  return describeRuntime(toLibraryConfig(config));
}

export function createRetromRpgRuntime(config: RpgRuntimeConfig, options: RetromRpgRuntimeOptions) {
  validateRpgRuntimeConfig(config);
  return createRuntime(toLibraryConfig(config), withDiagnostics(options));
}

export async function mountRetromRpgRuntime(
  config: RpgRuntimeConfig,
  target: HTMLElement,
  options: RetromRpgRuntimeOptions,
) {
  validateRpgRuntimeConfig(config);
  const runtime = await mountRuntime(toLibraryConfig(config), target, withDiagnostics(options));
  return {
    runtime,
    playerInstance: retromRuntimePlayerInstance(runtime, target, playerBridgeOptions(config)),
  };
}

function toLibraryConfig(config: RpgRuntimeConfig): LibraryRuntimeConfig {
  return {
    sessionId: config.launchId,
    generation: config.generation,
    validationPurpose: config.purpose === "RPG_RUNTIME_VALIDATION",
    expectedRestorePosition: restorePosition(config),
    adapter: toLibraryAdapter(config.adapter),
  };
}

function toLibraryAdapter(adapter: RpgRuntimeConfig["adapter"]): LibraryRuntimeConfig["adapter"] {
  if (adapter.adapterKind !== "NATIVE_WEB") {return adapter;}
  return {
    ...adapter,
    cleanupUrl: new URL("/__retrom/cleanup", adapter.uniqueOrigin).href,
  };
}

function restorePosition(config: RpgRuntimeConfig): RuntimePosition | null {
  if (!config.checkpoint || !config.runtimeValidation) {return null;}
  const gate = config.runtimeValidation.machineGates.find((entry) => entry.gate === "SAVE_POINT_RECORDED");
  const evidence = gate?.evidence;
  if (gate?.status !== "PASSED" || !isPosition(evidence)) {return null;}
  return evidence;
}

function isPosition(value: unknown): value is RpgPosition {
  if (!value || typeof value !== "object" || Array.isArray(value)) {return false;}
  return "mapId" in value && Number.isSafeInteger(value.mapId) &&
    "playerX" in value && Number.isSafeInteger(value.playerX) &&
    "playerY" in value && Number.isSafeInteger(value.playerY) &&
    "fixtureState" in value && Number.isSafeInteger(value.fixtureState);
}

function withDiagnostics(options: RetromRpgRuntimeOptions): RuntimeOptions {
  return {
    ...options,
    onDiagnostic: (diagnostic) => {
      window.dispatchEvent(new CustomEvent("retrom:runtime-diagnostic", { detail: diagnostic }));
    },
  };
}

function playerBridgeOptions(config: RpgRuntimeConfig) {
  if (config.adapter.adapterKind === "MKXP_LIBRETRO_WEB") {
    return {
      checkpointFormat: "mkxp-state-compact-v1",
      payloadKind: "RUNTIME_STATE" as const,
      rpgPositionProbe: true,
      validationPurpose: config.purpose === "RPG_RUNTIME_VALIDATION",
    };
  }
  return {
    checkpointFormat: config.adapter.adapterKind === "EASYRPG_WEB"
      ? "easyrpg-save-bundle-v1"
      : "native-save-bundle-v1",
    payloadKind: "NATIVE_SAVE_BUNDLE_V1" as const,
    rpgPositionProbe: true,
    validationPurpose: config.purpose === "RPG_RUNTIME_VALIDATION",
  };
}
