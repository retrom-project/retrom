import type { PlayerConfig } from "./adapters/ejs-4.2.3-v2";
import type { OnsLaunchConfig } from "./ons-runtime";
import type { KiriKiriLaunchConfig } from "./kirikiri-runtime";
import type { ButterscotchLaunchConfig } from "./butterscotch-runtime";
import type { RpgRuntimeConfig } from "./rpg-runtime";
import { readBoundedResponse } from "./player-shell-model";
import type { PlayerDebugRuntime } from "./player-chrome";
import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";

type RpgRuntimeDescription = { runtimeBaseUrl: string; requiresThreads: boolean };

export function onsShellConfig(config: OnsLaunchConfig): PlayerConfig {
  return {
    mode: "single", launchId: config.launchId, emulatorjsVersion: config.runtimeVersion,
    playerAdapterId: config.adapter.adapterId, core: config.coreId, runtimeCore: config.coreId,
    coreName: config.coreName, coreArtifactId: config.artifactId, emulatorGameId: 0,
    gameName: config.launchId, gameTitle: config.gameTitle, platformName: config.platformName,
    runtimeBaseUrl: config.adapter.runtimeBaseUrl, loaderUrl: "", gameUrl: "", biosUrl: null,
    parentUrl: null, stateUrl: config.checkpoint?.payloadUrl ?? null, inputMode: "STANDARD",
    startupActions: [], requiresThreads: false, runtimePathOverrides: {}, defaultCoreOptions: {},
    externalFiles: {}, discSet: null, dosEntry: null, warnings: config.warnings,
    returnTo: config.returnTo, netplay: null,
  };
}

export function kirikiriShellConfig(config: KiriKiriLaunchConfig): PlayerConfig {
  return {
    mode: "single", launchId: config.launchId, emulatorjsVersion: config.runtimeVersion,
    playerAdapterId: config.adapter.adapterId, core: config.coreId, runtimeCore: config.coreId,
    coreName: config.coreName, coreArtifactId: config.artifactId, emulatorGameId: 0,
    gameName: config.launchId, gameTitle: config.gameTitle, platformName: config.platformName,
    runtimeBaseUrl: config.adapter.runtimeBaseUrl, loaderUrl: "", gameUrl: "", biosUrl: null,
    parentUrl: null, stateUrl: config.checkpoint?.payloadUrl ?? null, inputMode: "STANDARD",
    startupActions: [], requiresThreads: true, runtimePathOverrides: {}, defaultCoreOptions: {},
    externalFiles: {}, discSet: null, dosEntry: null, warnings: config.warnings,
    returnTo: config.returnTo, netplay: null,
  };
}

export function butterscotchShellConfig(config: ButterscotchLaunchConfig): PlayerConfig {
  return {
    mode: "single", launchId: config.launchId, emulatorjsVersion: config.runtimeVersion,
    playerAdapterId: config.adapter.adapterId, core: config.coreId, runtimeCore: config.coreId,
    coreName: config.coreName, coreArtifactId: config.artifactId, emulatorGameId: 0,
    gameName: config.launchId, gameTitle: config.gameTitle, platformName: config.platformName,
    runtimeBaseUrl: config.adapter.runtimeBaseUrl, loaderUrl: "", gameUrl: "", biosUrl: null,
    parentUrl: null, stateUrl: config.checkpoint?.payloadUrl ?? null, inputMode: "STANDARD",
    startupActions: [], requiresThreads: true, runtimePathOverrides: {}, defaultCoreOptions: {},
    externalFiles: {}, discSet: null, dosEntry: null, warnings: config.warnings,
    returnTo: config.returnTo, netplay: null,
  };
}

export function rpgShellConfig(config: RpgRuntimeConfig, runtime: RpgRuntimeDescription): PlayerConfig {
  return {
    mode: "single", launchId: config.launchId, emulatorjsVersion: config.routeKey,
    playerAdapterId: config.adapter.adapterId, core: config.coreId, runtimeCore: config.coreId,
    coreName: config.coreName, coreArtifactId: config.artifactId, emulatorGameId: 0,
    gameName: config.launchId, gameTitle: config.gameTitle, platformName: config.platformName,
    runtimeBaseUrl: runtime.runtimeBaseUrl, loaderUrl: "", gameUrl: "", biosUrl: null, parentUrl: null,
    stateUrl: config.checkpoint?.payloadUrl ?? null, inputMode: "STANDARD", startupActions: [],
    requiresThreads: runtime.requiresThreads, runtimePathOverrides: {}, defaultCoreOptions: {},
    externalFiles: {}, discSet: null, dosEntry: null, warnings: config.warnings,
    returnTo: config.returnTo, netplay: null,
  };
}

export function rpgDebugRuntime(config: RpgRuntimeConfig): PlayerDebugRuntime {
  return {
    runtimeFamily: "RPGMAKER", coreId: config.coreId, coreArtifactId: config.artifactId,
    emulatorJSVersion: config.routeKey, playerAdapterId: config.adapter.adapterId,
    inputMode: "STANDARD", crossOriginIsolated: window.crossOriginIsolated,
    sharedArrayBuffer: typeof SharedArrayBuffer !== "undefined",
  };
}

export async function fetchOnsCheckpoint(config: OnsLaunchConfig, signal: AbortSignal) {
  return fetchCheckpoint(config.checkpoint?.payloadUrl, 64 * 1024 * 1024, signal);
}

export async function fetchKiriKiriCheckpoint(config: KiriKiriLaunchConfig, signal: AbortSignal) {
  return fetchCheckpoint(config.checkpoint?.payloadUrl, 64 * 1024 * 1024, signal);
}

export async function fetchButterscotchCheckpoint(config: ButterscotchLaunchConfig, signal: AbortSignal) {
  return fetchCheckpoint(config.checkpoint?.payloadUrl, 16 * 1024 * 1024, signal);
}

export async function fetchRpgCheckpoint(config: RpgRuntimeConfig, signal: AbortSignal) {
  return fetchCheckpoint(config.checkpoint?.payloadUrl, 256 * 1024 * 1024, signal);
}

async function fetchCheckpoint(payloadUrl: string | undefined, maximumBytes: number, signal: AbortSignal) {
  if (!payloadUrl) {return null;}
  const response = await fetch(payloadUrl, { credentials: "same-origin", cache: "no-store", signal });
  if (!response.ok) {throw new Error("PLAYER_SAVE_STATE_UNAVAILABLE");}
  const payload = await readBoundedResponse(response, maximumBytes);
  if (!payload.byteLength) {throw new Error("PLAYER_SAVE_STATE_UNAVAILABLE");}
  return payload;
}

export function observedRuntimeDiscCount(instance: EmulatorInstance | undefined) {
  const value = instance?.gameManager?.getDiskCount?.();
  return typeof value === "number" && Number.isInteger(value) && value >= -1 && value <= 64 ? value : null;
}
