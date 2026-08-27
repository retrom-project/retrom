import type { RpgRuntimeConfig } from "./contract";
import { rpgValidationGates, validateRpgPosition, type RpgGate } from "../rpg-validation-protocol";

export const rpgRuntimeRoutes = [
  {
    coreId: "rpgmaker_2000", generation: "RPG2000", routeKey: "RPG2000_EASYRPG",
    adapterKind: "EASYRPG_WEB", adapterId: "easyrpg-web", engineMode: "rpg2k",
    runtimeVersion: "v0.3.0",
  },
  {
    coreId: "rpgmaker_2003", generation: "RPG2003", routeKey: "RPG2003_EASYRPG",
    adapterKind: "EASYRPG_WEB", adapterId: "easyrpg-web", engineMode: "rpg2k3",
    runtimeVersion: "v0.3.0",
  },
  {
    coreId: "rpgmaker_xp", generation: "RPGXP", routeKey: "RPGXP_MKXP",
    adapterKind: "MKXP_LIBRETRO_WEB", adapterId: "mkxp-libretro-web", rgssVersion: 1,
    runtimeVersion: "v0.3.0",
  },
  {
    coreId: "rpgmaker_vx", generation: "RPGVX", routeKey: "RPGVX_MKXP",
    adapterKind: "MKXP_LIBRETRO_WEB", adapterId: "mkxp-libretro-web", rgssVersion: 2,
    runtimeVersion: "v0.3.0",
  },
  {
    coreId: "rpgmaker_vx_ace", generation: "RPGVXACE", routeKey: "RPGVXACE_MKXP",
    adapterKind: "MKXP_LIBRETRO_WEB", adapterId: "mkxp-libretro-web", rgssVersion: 3,
    runtimeVersion: "v0.3.0",
  },
  {
    coreId: "rpgmaker_mv", generation: "RPGMV", routeKey: "RPGMV_NATIVE",
    adapterKind: "NATIVE_WEB", adapterId: "native-web", bridgeProfile: "RPGMV",
    runtimeVersion: "v0.3.0",
  },
  {
    coreId: "rpgmaker_mz", generation: "RPGMZ", routeKey: "RPGMZ_NATIVE",
    adapterKind: "NATIVE_WEB", adapterId: "native-web", bridgeProfile: "RPGMZ",
    runtimeVersion: "v0.3.0",
  },
] as const;

type Route = typeof rpgRuntimeRoutes[number];

const routeByIdentity = new Map<string, Route>(
  rpgRuntimeRoutes.map((route) => [`${route.coreId}\u0000${route.routeKey}`, route]),
);
const checkpointReasons = new Set([
  "NOT_ON_MAP", "SAVE_DISABLED", "MESSAGE_ACTIVE", "EVENT_ACTIVE", "BUSY", "RUNTIME_NOT_READY",
  "RUNTIME_FAILED", "CHECKPOINT_ALREADY_CREATED", "NETPLAY_UNSUPPORTED",
]);
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
const digest = /^[0-9a-f]{64}$/u;

export function isRpgRuntimeConfig(value: unknown): value is RpgRuntimeConfig {
  return Boolean(value && typeof value === "object" && !Array.isArray(value) &&
    (value as { runtimeFamily?: unknown }).runtimeFamily === "RPGMAKER");
}

export function validateRpgRuntimeConfig(config: RpgRuntimeConfig) {
  if (!exactKeys(config, [
    "adapter", "artifactId", "checkpoint", "checkpointAvailability", "coreId", "coreName", "gameTitle",
    "generation", "launchId", "mode", "platformName", "protocolVersion", "purpose", "returnTo", "routeKey",
    "runtimeFamily", "runtimeValidation", "warnings",
  ]) || !isRecord(config.adapter)) {throw new Error("PLAYER_RPG_CONFIG_INVALID");}
  const route = routeByIdentity.get(`${config.coreId}\u0000${config.routeKey}`);
  if (!validCommonConfig(config) || !route || route.generation !== config.generation ||
    route.routeKey !== config.routeKey || route.adapterKind !== config.adapter.adapterKind ||
    route.adapterId !== config.adapter.adapterId) {
    throw new Error("PLAYER_RPG_CONFIG_INVALID");
  }
  const valid = config.adapter.adapterKind === "EASYRPG_WEB"
    ? validateEasy(config, route)
    : config.adapter.adapterKind === "MKXP_LIBRETRO_WEB"
      ? validateMkxp(config, route)
      : validateNative(config, route);
  if (!valid) {throw new Error("PLAYER_RPG_CONFIG_INVALID");}
}

function validCommonConfig(config: RpgRuntimeConfig) {
  return [
    config.runtimeFamily === "RPGMAKER", config.protocolVersion === 1, config.mode === "single",
    config.purpose === "PRODUCT" || config.purpose === "RPG_RUNTIME_VALIDATION", uuid.test(config.launchId),
    uuid.test(config.artifactId), boundedText(config.coreName, 200), boundedText(config.gameTitle, 200),
    config.platformName === "RPG Maker", validAppPath(config.returnTo), validWarnings(config.warnings),
    validAvailability(config.checkpointAvailability), validCheckpoint(config), validRuntimeValidation(config),
  ].every(Boolean);
}

function validRuntimeValidation(config: RpgRuntimeConfig) {
  const validation = config.runtimeValidation;
  if (config.purpose === "PRODUCT") {return validation === null;}
  if (!validation || !exactKeys(validation, [
    "checkpointEvidence", "lastGateSequence", "machineGates", "originalLaunchId", "restoreLaunchId",
    "restoreScreenshotUploaded", "state", "validationId",
  ])) {return false;}
  const currentLaunch = validation.originalLaunchId === config.launchId || validation.restoreLaunchId === config.launchId;
  const eventCount = validation.machineGates.reduce((count, gate) =>
    count + (gate.status === "IN_PROGRESS" ? 1 : gate.status === "NOT_STARTED" ? 0 : 2), 0);
  return [
    uuid.test(validation.validationId), uuid.test(validation.originalLaunchId),
    validation.restoreLaunchId === null || uuid.test(validation.restoreLaunchId), currentLaunch,
    Number.isSafeInteger(validation.lastGateSequence), validation.lastGateSequence === eventCount,
    validation.lastGateSequence >= 0 && validation.lastGateSequence <= 28,
    typeof validation.restoreScreenshotUploaded === "boolean", validValidationState(validation.state),
    validCheckpointEvidence(validation.checkpointEvidence), validMachineGates(validation.machineGates),
  ].every(Boolean);
}

function validMachineGates(gates: NonNullable<RpgRuntimeConfig["runtimeValidation"]>["machineGates"]) {
  return Array.isArray(gates) && gates.length === rpgValidationGates.length &&
    gates.every((gate, index) => validMachineGate(gate, index));
}

type ValidationMachineGate = NonNullable<RpgRuntimeConfig["runtimeValidation"]>["machineGates"][number];

function validMachineGate(gate: ValidationMachineGate, index: number) {
  if (!exactKeys(gate, ["begunAtMs", "completedAtMs", "evidence", "failureCode", "gate", "status"]) ||
    gate.gate !== rpgValidationGates[index] ||
    gate.begunAtMs !== null && !nonNegativeInteger(gate.begunAtMs) ||
    gate.completedAtMs !== null && !nonNegativeInteger(gate.completedAtMs)) {return false;}
  if (gate.status === "NOT_STARTED") {return validNotStartedGate(gate);}
  if (gate.status === "IN_PROGRESS") {return validInProgressGate(gate);}
  return validTerminalGate(gate);
}

function validNotStartedGate(gate: ValidationMachineGate) {
  return gate.begunAtMs === null && gate.completedAtMs === null && gate.evidence === null &&
    gate.failureCode === null;
}

function validInProgressGate(gate: ValidationMachineGate) {
  return gate.begunAtMs !== null && gate.completedAtMs === null && gate.evidence === null &&
    gate.failureCode === null;
}

function validTerminalGate(gate: ValidationMachineGate) {
  const terminal = gate.begunAtMs !== null && gate.completedAtMs !== null &&
    gate.completedAtMs >= gate.begunAtMs && validGateEvidence(gate.gate, gate.evidence);
  return terminal && (gate.status === "PASSED" ? gate.failureCode === null : typeof gate.failureCode === "string");
}

function validGateEvidence(gate: RpgGate, evidence: unknown) {
  const positionGate = gate === "INITIAL_POSITION_RECORDED" || gate === "SAVE_POINT_RECORDED" ||
    gate === "POST_SAVE_STATE_DIVERGED" || gate === "RESTORE_POSITION_VERIFIED" || gate === "RESTORE_INPUT";
  if (!positionGate) {return evidence !== null && typeof evidence === "object" && !Array.isArray(evidence);}
  if (!evidence || typeof evidence !== "object" || Array.isArray(evidence) ||
    !exactKeys(evidence, ["fixtureState", "mapId", "playerX", "playerY"])) {return false;}
  return validateRpgPosition(evidence as { mapId: number; playerX: number; playerY: number; fixtureState: number });
}

function validCheckpointEvidence(value: NonNullable<RpgRuntimeConfig["runtimeValidation"]>["checkpointEvidence"]) {
  if (value === null) {return true;}
  return exactKeys(value, ["payloadKind", "sha256", "sizeBytes"]) &&
    (value.payloadKind === "RUNTIME_STATE" || value.payloadKind === "NATIVE_SAVE_BUNDLE_V1") &&
    Number.isSafeInteger(value.sizeBytes) && value.sizeBytes >= 1 && value.sizeBytes <= 268435456 &&
    digest.test(value.sha256);
}

function validValidationState(value: string) {
  return new Set([
    "CREATED", "STARTING", "RUNNING", "CHECKPOINTED", "RESTORED", "AWAITING_DECISION",
    "PASSED", "FAILED", "EXPIRED",
  ]).has(value);
}

function nonNegativeInteger(value: number) {
  return Number.isSafeInteger(value) && value >= 0;
}

function validCheckpoint(config: RpgRuntimeConfig) {
  if (config.checkpoint === null) {return true;}
  return exactKeys(config.checkpoint, ["payloadKind", "payloadUrl"]) &&
    (config.checkpoint.payloadKind === "RUNTIME_STATE" || config.checkpoint.payloadKind === "NATIVE_SAVE_BUNDLE_V1") &&
    config.checkpoint.payloadUrl === `/runtime/launches/${config.launchId}/state` && validAppUrl(config.checkpoint.payloadUrl);
}

function validateEasy(config: RpgRuntimeConfig, route: Route) {
  const adapter = config.adapter;
  if (![adapter.adapterKind === "EASYRPG_WEB", "engineMode" in route, exactKeys(adapter, [
    "adapterId", "adapterKind", "checkpointSlot", "engineMode", "projectIndexUrl", "projectRootUrl",
    "rtpArchive", "runtimeBaseUrl",
  ])].every(Boolean) || adapter.adapterKind !== "EASYRPG_WEB" || !("engineMode" in route)) {return false;}
  const root = `/runtime/rpg-project/${config.launchId}/`;
  const mountPath = route.engineMode === "rpg2k" ? "/data/rtp/2000" : "/data/rtp/2003";
  const runtime = `/runtime/rpgmaker/${route.runtimeVersion}/`;
  return [
    adapter.engineMode === route.engineMode, adapter.runtimeBaseUrl === runtime,
    validAppUrl(adapter.runtimeBaseUrl), adapter.projectRootUrl === root, validAppUrl(adapter.projectRootUrl),
    adapter.projectIndexUrl === `${root}index.json`, validAppUrl(adapter.projectIndexUrl),
    adapter.checkpointSlot === 100, validEasyRtp(adapter.rtpArchive, root, mountPath),
  ].every(Boolean);
}

function validateMkxp(config: RpgRuntimeConfig, route: Route) {
  const adapter = config.adapter;
  if (adapter.adapterKind !== "MKXP_LIBRETRO_WEB" || !("rgssVersion" in route)) {return false;}
  if (![exactKeys(adapter, [
    "adapterId", "adapterKind", "core", "projectArchive", "rgssVersion", "rtpArchives", "runtimeBaseUrl",
    "stateBufferBytes",
  ]), exactKeys(adapter.core, [
    "artifactSetSha256", "jsSha256", "jsSizeBytes", "jsUrl", "wasmSha256", "wasmSizeBytes", "wasmUrl",
  ]),
  exactKeys(adapter.projectArchive, ["sha256", "sizeBytes", "url"])].every(Boolean)) {return false;}
  const runtime = `/runtime/rpgmaker/${route.runtimeVersion}/`;
  const root = `/runtime/rpg-project/${config.launchId}/`;
  return [
    adapter.rgssVersion === route.rgssVersion, adapter.stateBufferBytes === 268435456,
    adapter.runtimeBaseUrl === runtime, validAppUrl(adapter.runtimeBaseUrl),
    adapter.core.jsUrl === `${runtime}mkxp-z_libretro.js`, validAppUrl(adapter.core.jsUrl),
    adapter.core.wasmUrl === `${runtime}mkxp-z_libretro.wasm`, validAppUrl(adapter.core.wasmUrl),
    Number.isSafeInteger(adapter.core.jsSizeBytes), adapter.core.jsSizeBytes >= 1,
    adapter.core.jsSizeBytes <= 1048576, digest.test(adapter.core.jsSha256),
    Number.isSafeInteger(adapter.core.wasmSizeBytes), adapter.core.wasmSizeBytes >= 1,
    adapter.core.wasmSizeBytes <= 67108864, digest.test(adapter.core.wasmSha256),
    digest.test(adapter.core.artifactSetSha256), adapter.projectArchive.url === `${root}__retrom__/game.mkxpz`,
    validArchive(adapter.projectArchive, root), validMkxpRtps(adapter.rtpArchives, root),
  ].every(Boolean);
}

function validateNative(config: RpgRuntimeConfig, route: Route) {
  const adapter = config.adapter;
  if (![adapter.adapterKind === "NATIVE_WEB", "bridgeProfile" in route, exactKeys(adapter, [
    "adapterId", "adapterKind", "bootstrapTicket", "bootstrapUrl", "bridgeProfile", "uniqueOrigin",
  ])].every(Boolean) || adapter.adapterKind !== "NATIVE_WEB" || !("bridgeProfile" in route)) {return false;}
  if (![adapter.bridgeProfile === route.bridgeProfile,
    /^[A-Za-z0-9_-]{43,128}$/u.test(adapter.bootstrapTicket)].every(Boolean)) {return false;}
  let origin: URL;
  let bootstrap: URL;
  try {origin = new URL(adapter.uniqueOrigin); bootstrap = new URL(adapter.bootstrapUrl);}
  catch {return false;}
  const localhost = origin.protocol === "http:" && origin.port !== "" &&
    origin.hostname === `${config.launchId}.rpg.localhost`;
  return [
    origin.protocol === "https:" || localhost, origin.origin === adapter.uniqueOrigin,
    !origin.username, !origin.password, !origin.pathname.slice(1), !origin.search, !origin.hash,
    bootstrap.origin === origin.origin, bootstrap.pathname === "/__retrom/bootstrap",
    !bootstrap.username, !bootstrap.password, !bootstrap.search, !bootstrap.hash,
  ].every(Boolean);
}

function validEasyRtp(
  archive: Extract<RpgRuntimeConfig["adapter"], { adapterKind: "EASYRPG_WEB" }>["rtpArchive"],
  root: string,
  mountPath: string,
) {
  if (archive === null) {return true;}
  const url = typeof archive.url === "string" ? archive.url : "";
  return [
    exactKeys(archive, ["mountPath", "sha256", "url"]), digest.test(archive.sha256), validAppUrl(url),
    validAppPrefix(url, root), archive.mountPath === mountPath,
  ].every(Boolean);
}

function validMkxpRtps(
  archives: Extract<RpgRuntimeConfig["adapter"], { adapterKind: "MKXP_LIBRETRO_WEB" }>["rtpArchives"],
  root: string,
) {
  return Array.isArray(archives) && archives.length <= 3 && archives.every((archive) => [
    exactKeys(archive, ["declaredName", "sha256", "sizeBytes", "url"]),
    boundedText(archive.declaredName, 512), validArchive(archive, root),
  ].every(Boolean));
}

function validArchive(archive: { url: string; sha256: string; sizeBytes: number }, root: string) {
  const url = typeof archive.url === "string" ? archive.url : "";
  return [
    typeof archive.url === "string", validAppPrefix(url, root), validAppUrl(url),
    typeof archive.sha256 === "string", digest.test(archive.sha256),
    Number.isSafeInteger(archive.sizeBytes), archive.sizeBytes >= 0,
  ].every(Boolean);
}

function validAvailability(value: RpgRuntimeConfig["checkpointAvailability"]) {
  return exactKeys(value, ["available", "reason"]) && typeof value.available === "boolean" &&
    (value.available ? value.reason === null : typeof value.reason === "string" && checkpointReasons.has(value.reason));
}

function validAppPath(value: string) {
  if (typeof value !== "string" || !value.startsWith("/") || value.startsWith("//") || value.length > 2048) {return false;}
  try {
    const url = new URL(value, window.location.origin);
    return url.origin === window.location.origin && !url.username && !url.password && !url.hash;
  } catch {return false;}
}

function validAppUrl(value: unknown) {
  if (typeof value !== "string" || value.startsWith("//")) {return false;}
  try {
    const url = new URL(value, window.location.origin);
    return url.origin === window.location.origin && !url.username && !url.password && !url.search && !url.hash;
  } catch {return false;}
}

function validAppPrefix(value: string, prefix: string) {
  try {return new URL(value, window.location.origin).pathname.startsWith(prefix);}
  catch {return false;}
}

function boundedText(value: unknown, maximum: number): value is string {
  return typeof value === "string" && value.length > 0 && value.length <= maximum;
}

function validWarnings(value: unknown) {
  return Array.isArray(value) && value.length <= 32 &&
    value.every((warning) => typeof warning === "string" && warning.length <= 500);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

function exactKeys(value: unknown, expected: string[]) {
  return isRecord(value) && Object.keys(value).sort().join("\u0000") === [...expected].sort().join("\u0000");
}
