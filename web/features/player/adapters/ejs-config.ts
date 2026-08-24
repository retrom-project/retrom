import type { EmulatorInstance, PlayerConfig } from "./ejs-4.2.3-v2";

function safeVirtualPath(value: string) {
  if (!value.startsWith("/") || value.length > 512 || value.includes("\\") || value.includes("?") ||
    value.includes("#") || value.includes("//")) {return false;}
  return value.slice(1).split("/").every((segment) => segment !== "" && segment !== "." && segment !== "..");
}

export function validatedExternalFiles(config: PlayerConfig): Record<string, string> {
  const entries = Object.entries(config.externalFiles);
  if (entries.length > 16) {throw new Error("PLAYER_EXTERNAL_FILES_INVALID");}
  const result: Record<string, string> = {};
  for (const [virtualPath, source] of entries) {
    const externalPrefix = `/runtime/launches/${config.launchId}/external-files/`;
    const logicalName = source.startsWith(externalPrefix) ? source.slice(externalPrefix.length) : "";
    if (!safeVirtualPath(virtualPath) || source.length > 1024 ||
      !source.startsWith(externalPrefix) || !/^[A-Za-z0-9_(). -]{1,255}$/.test(logicalName) ||
      logicalName === "." || logicalName === "..") {
      throw new Error("PLAYER_EXTERNAL_FILES_INVALID");
    }
    result[virtualPath] = source;
  }
  return result;
}

function validDiscSetHeader(config: PlayerConfig) {
  const discSet = config.discSet;
  return discSet?.contentKind === "MULTI_DISC_M3U_V1" && Number.isInteger(discSet.count) &&
    discSet.count >= 2 && discSet.count <= 8 && Array.isArray(discSet.entries) &&
    discSet.entries.length === discSet.count && Number.isInteger(discSet.initialDiscIndex) &&
    discSet.initialDiscIndex >= 0 && discSet.initialDiscIndex < discSet.count &&
    config.gameUrl === `/runtime/launches/${config.launchId}/game/playlist.m3u`;
}

function validDiscEntry(config: PlayerConfig, externalFiles: Record<string, string>, index: number) {
  const entry = config.discSet?.entries[index];
  const canonicalName = `disc-${String(index + 1).padStart(3, "0")}.chd`;
  return Boolean(entry && entry.index === index && entry.label === `光盘 ${index + 1}` &&
    entry.virtualPath === `/${canonicalName}` &&
    externalFiles[entry.virtualPath] === `/runtime/launches/${config.launchId}/external-files/${canonicalName}`);
}

export function validateDiscSet(config: PlayerConfig, externalFiles: Record<string, string>) {
  const discSet = config.discSet;
  if (discSet === undefined || discSet === null) {return;}
  if (!validDiscSetHeader(config)) {throw new Error("PLAYER_DISC_SET_INVALID");}
  for (let index = 0; index < discSet.count; index += 1) {
    if (!validDiscEntry(config, externalFiles, index)) {throw new Error("PLAYER_DISC_SET_INVALID");}
  }
}

export type DiscState = { count: number; currentIndex: number };

export function readDiscState(instance: EmulatorInstance, expectedCount?: number): DiscState {
  const count = instance.gameManager?.getDiskCount?.();
  const currentIndex = instance.gameManager?.getCurrentDisk?.();
  if (!Number.isInteger(count) || !Number.isInteger(currentIndex) || count === undefined || currentIndex === undefined ||
    count < 2 || count > 8 || currentIndex < 0 || currentIndex >= count ||
    expectedCount !== undefined && count !== expectedCount) {
    throw new Error("PLAYER_DISC_SET_INVALID");
  }
  return { count, currentIndex };
}

export function switchDisc(instance: EmulatorInstance, targetIndex: number, expectedCount: number): DiscState {
  if (!Number.isInteger(targetIndex) || targetIndex < 0 || targetIndex >= expectedCount) {
    throw new Error("PLAYER_DISC_INDEX_INVALID");
  }
  const before = readDiscState(instance, expectedCount);
  if (targetIndex === before.currentIndex) {return before;}
  const setCurrentDisk = instance.gameManager?.setCurrentDisk?.bind(instance.gameManager);
  if (!setCurrentDisk) {throw new Error("PLAYER_DISC_SWITCH_UNAVAILABLE");}
  setCurrentDisk(targetIndex);
  const after = readDiscState(instance, expectedCount);
  if (after.currentIndex !== targetIndex) {throw new Error("PLAYER_DISC_SWITCH_FAILED");}
  return after;
}

export function switchDiscPreservingPause(
  instance: EmulatorInstance,
  targetIndex: number,
  expectedCount: number,
): DiscState {
  const manager = instance.gameManager;
  if (!manager?.toggleMainLoop) {throw new Error("PLAYER_DISC_API_UNAVAILABLE");}
  const wasPaused = instance.paused === true;
  manager.toggleMainLoop(false);
  try {
    return switchDisc(instance, targetIndex, expectedCount);
  } finally {
    manager.toggleMainLoop(!wasPaused);
    instance.paused = wasPaused;
  }
}

export function initializeMultiDiscSettings(instance: EmulatorInstance) {
  if (instance.allSettings === undefined) {
    instance.allSettings = {};
    return;
  }
  if (Object.prototype.toString.call(instance.allSettings) !== "[object Object]") {
    throw new Error("PLAYER_DISC_SETTINGS_INVALID");
  }
}

export const netplayProfilePredictionFrames: Readonly<Record<string, number>> = Object.freeze({
  "fceumm-423-v1": 8,
  "fbneo-423-v1": 0,
  "snes9x-423-v1": 0,
  "mame2003-423-override-v1": 0,
  "mame2003-plus-423-v1": 0,
  "fbalpha2012-cps1-423-v1": 0,
  "fbalpha2012-cps2-423-v1": 0,
  "nestopia-423-v1": 0,
});

function expectedPredictionFrames(profileID: string) {
  return Object.hasOwn(netplayProfilePredictionFrames, profileID)
    ? netplayProfilePredictionFrames[profileID]
    : null;
}

function validNetplayConfig(config: PlayerConfig) {
  const netplay = config.netplay;
  const profile = netplay?.netplayProfile;
  if (!netplay || !profile) {return false;}
  return validNetplayRuntime(config) && validNetplayRoute(config, netplay) &&
    validNetplayProfileIdentity(config, profile) && validNetplayProfileLimits(netplay, profile);
}

function validNetplayRuntime(config: PlayerConfig) {
  return config.emulatorjsVersion === "4.2.3" && config.playerAdapterId === "ejs-4.2.3-v2" &&
    config.stateUrl === null && !config.discSet;
}

function validNetplayRoute(config: PlayerConfig, netplay: NonNullable<PlayerConfig["netplay"]>) {
  return netplay.playerNo >= 1 && netplay.playerNo <= 4 &&
    netplay.runtimeSocketUrl === `/runtime/netplay/rooms/${netplay.roomId}/socket`;
}

function validNetplayProfileIdentity(
  config: PlayerConfig,
  profile: NonNullable<PlayerConfig["netplay"]>["netplayProfile"],
) {
  return profile.schemaVersion === 1 && profile.protocolVersion === "retrom-netplay-v2" &&
    profile.playerAdapterId === config.playerAdapterId && profile.netplayAdapterId === "ejs-netplay-4.2.3-v1" &&
    profile.coreArtifactId === config.coreArtifactId && profile.emulatorjsVersion === config.emulatorjsVersion &&
    profile.gameVariantRevisionId.length > 0 && /^[0-9a-f]{64}$/.test(profile.coreArtifactSha256) &&
    /^[0-9a-f]{64}$/.test(profile.sourceManifestDigest) && /^[0-9a-f]{64}$/.test(profile.dependencySnapshotDigest) &&
    Boolean(profile.defaultCoreOptions);
}

function validNetplayProfileLimits(
  netplay: NonNullable<PlayerConfig["netplay"]>,
  profile: NonNullable<PlayerConfig["netplay"]>["netplayProfile"],
) {
  return netplay.playerNo <= profile.maxPlayers && profile.controlCount === 24 &&
    profile.maxPredictionFrames === expectedPredictionFrames(profile.profileId) && profile.maxRollbackFrames === 120 &&
    profile.checkpointEveryFrames === 120 && profile.canonicalHistoryFrames === 600 && profile.maxStateBytes === 1048576;
}

function validRuntimePaths(config: PlayerConfig) {
  const entries = Object.entries(config.runtimePathOverrides);
  return Boolean(config.runtimePathOverrides) && entries.length === 1 && entries.every(([name, source]) =>
    /^[A-Za-z0-9_.-]+-wasm\.data$/.test(name) && !name.includes("..") &&
    source.startsWith(`/runtime/emulatorjs/${config.emulatorjsVersion}/`));
}

function validCoreOptions(config: PlayerConfig) {
  const entries = Object.entries(config.defaultCoreOptions);
  return Boolean(config.defaultCoreOptions) && entries.length <= 32 && entries.every(([name, value]) =>
    !["__proto__", "constructor", "prototype"].includes(name) && /^[\x20-\x7E]{1,128}$/.test(name) &&
    /^[\x20-\x7E]{0,128}$/.test(value));
}

function validStartupActions(config: PlayerConfig) {
  return Array.isArray(config.startupActions) && config.startupActions.length <= 4 &&
    config.startupActions.every((action) => action.event === "GAME_START" && action.kind === "PRESS_CONTROL" &&
      Number.isInteger(action.delayMs) && action.delayMs >= 0 && action.delayMs <= 30_000 &&
      Number.isInteger(action.player) && action.player >= 0 && action.player <= 3 &&
      Number.isInteger(action.control) && action.control >= 0 && action.control <= 255 &&
      Number.isInteger(action.durationMs) && action.durationMs >= 1 && action.durationMs <= 1_000);
}

export function validateConfig(config: PlayerConfig) {
  if (config.mode !== "single" && config.mode !== "netplay") {throw new Error("PLAYER_MODE_INVALID");}
  if (config.mode === "single" && config.netplay !== null) {throw new Error("PLAYER_NETPLAY_CONFIG_INVALID");}
  if (config.mode === "netplay" && !validNetplayConfig(config)) {throw new Error("PLAYER_NETPLAY_CONFIG_INVALID");}
  if (!/^[a-z0-9_]{1,64}$/.test(config.runtimeCore)) {throw new Error("PLAYER_RUNTIME_CORE_INVALID");}
  if (!validRuntimePaths(config)) {throw new Error("PLAYER_RUNTIME_PATHS_INVALID");}
  if (config.inputMode !== "STANDARD" && config.inputMode !== "POINTER") {throw new Error("PLAYER_INPUT_MODE_INVALID");}
  if (!validCoreOptions(config)) {throw new Error("PLAYER_CORE_OPTIONS_INVALID");}
  if (!validStartupActions(config)) {throw new Error("PLAYER_STARTUP_ACTION_INVALID");}
}
