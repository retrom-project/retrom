import type {LaunchEnvelopeV1, RuntimeCapabilitiesV1, RuntimeResourceV1, TargetOptionsV1} from "./contract";
import {parseCanonicalJSON} from "./canonical-json";
import {playerRuntimeError} from "./errors";

const digest = /^[0-9a-f]{64}$/u;
const identity = /^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$/u;
const token = /^[a-z0-9](?:[a-z0-9.-]{0,62}[a-z0-9])?$/u;
const semver = /^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?$/u;

export function parseLaunchEnvelopeJSON(source: string): LaunchEnvelopeV1 {
  try {return validateLaunchEnvelopeBoundary(parseCanonicalJSON(source));}
  catch {return invalid();}
}

export function validateLaunchEnvelopeBoundary(value: unknown): LaunchEnvelopeV1 {
  if (!record(value) || !exactKeys(value, [
    "netplay", "resources", "restore", "runtime", "schemaVersion", "session", "targetOptions", "validation",
  ]) || value.schemaVersion !== 1 || !validSession(value.session)) {invalid();}
  const runtime = launchRuntime(value.runtime);
  if (!runtime || !validEnvelopeBody(value, runtime)) {invalid();}
  return value as unknown as LaunchEnvelopeV1;
}

function launchRuntime(value: unknown): LaunchEnvelopeV1["runtime"] | null {
  if (!record(value) || !exactKeys(value, [
    "bundleSha256", "capabilities", "checkpoint", "moduleSha256", "moduleUrl",
    "providerApiVersion", "providerId", "providerVersion", "runtimeBaseUrl", "targetId",
  ]) || !validRuntimeIdentity(value) || !validRuntimeDigests(value) || !validCapabilities(value.capabilities) ||
    !validCheckpoint(value.checkpoint, value.capabilities.checkpoint)) {return null;}
  return value as unknown as LaunchEnvelopeV1["runtime"];
}

function validRuntimeIdentity(value: Record<string, unknown>) {
  return value.providerApiVersion === 1 && validIdentity(value.providerId) && validIdentity(value.targetId) &&
    typeof value.providerVersion === "string" && semver.test(value.providerVersion);
}

function validRuntimeDigests(value: Record<string, unknown>) {
  return validDigest(value.bundleSha256) && validDigest(value.moduleSha256);
}

function validEnvelopeBody(value: Record<string, unknown>, runtime: LaunchEnvelopeV1["runtime"]) {
  const base = `/runtime/providers/${runtime.providerId}/${runtime.bundleSha256}/`;
  return runtime.runtimeBaseUrl === base && runtime.moduleUrl === `${base}client.mjs` &&
    validResources(value.resources) && validTargetOptions(value.targetOptions) &&
    validRestore(value.restore, runtime.checkpoint) && validValidation(value.validation, runtime.capabilities) &&
    validNetplay(value.netplay, runtime.capabilities.netplayPort, value.session);
}

function validSession(value: unknown) {
  return record(value) && exactKeys(value, [
    "coreName", "id", "mode", "platformName", "purpose", "returnTo", "title", "warnings",
  ]) && uuid(value.id) && ["PRODUCT", "REVIEW_PREVIEW", "RUNTIME_VALIDATION"].includes(String(value.purpose)) &&
    ["SINGLE", "NETPLAY"].includes(String(value.mode)) && bounded(value.title, 1, 500) &&
    bounded(value.platformName, 1, 200) && bounded(value.coreName, 1, 200) &&
    relativeURL(value.returnTo) && Array.isArray(value.warnings) &&
    value.warnings.length <= 16 && value.warnings.every((warning) => bounded(warning, 1, 200));
}

function validCapabilities(value: unknown): value is RuntimeCapabilitiesV1 {
  if (!record(value) || !exactKeys(value, [
    "checkpoint", "discSwitch", "frameCounter", "frameMode", "inputFilter", "nativeSettings", "netplayPort",
    "pause", "requiresThreads", "screenshot", "standardGamepad", "validationProbes", "videoModes", "volume",
  ])) {return false;}
  for (const key of [
    "checkpoint", "discSwitch", "frameCounter", "inputFilter", "nativeSettings", "netplayPort", "pause",
    "requiresThreads", "screenshot", "standardGamepad", "volume",
  ]) {if (typeof value[key] !== "boolean") {return false;}}
  return ["NONE", "SAME_ORIGIN_BLANK", "SAME_ORIGIN_RESOURCE", "ISOLATED_ORIGIN_RESOURCE"]
    .includes(String(value.frameMode)) && Array.isArray(value.validationProbes) &&
    sortedUnique(value.validationProbes) && value.validationProbes.every(validToken) &&
    Array.isArray(value.videoModes) && sortedUnique(value.videoModes) &&
    value.videoModes.every((mode) => ["original", "pixel", "smooth", "sharp-bilinear", "adaptive-sharpen"].includes(mode));
}

function validCheckpoint(value: unknown, enabled: boolean) {
  if (!enabled) {return value === null;}
  return record(value) && exactKeys(value, ["maxBytes", "readFormats", "writeFormat"]) &&
    positiveInteger(value.maxBytes) && validToken(value.writeFormat) && Array.isArray(value.readFormats) &&
    sortedUnique(value.readFormats) && value.readFormats.every(validToken) && value.readFormats.includes(value.writeFormat);
}

function validResources(value: unknown): value is RuntimeResourceV1[] {
  if (!Array.isArray(value) || value.length > 128) {return false;}
  const identities = new Set<string>();
  for (const item of value) {
    if (!record(item) || !validIdentity(item.role) || !nonNegativeInteger(item.ordinal) ||
      identities.has(`${item.role}\0${item.ordinal}`) || !validResource(item)) {return false;}
    identities.add(`${item.role}\0${item.ordinal}`);
  }
  const roles = new Set(value.map((item) => String(item.role)));
  return [...roles].every((role) => value.filter((item) => item.role === role)
    .every((item, index) => item.ordinal === index));
}

function validResource(value: Record<string, unknown>) {
  if (["ROM_BLOB", "SEEKABLE_BLOB", "PARENT_ARCHIVE", "WASM4_CART"].includes(String(value.kind))) {
    return validBlobResource(value);
  }
  if (value.kind === "FILE_TREE") {return validFileTreeResource(value);}
  if (value.kind === "NATIVE_WEB" || value.kind === "ISOLATED_WEB") {return validWebResource(value);}
  if (value.kind === "BIOS_BUNDLE" || value.kind === "EXTERNAL_FILE_SET") {return validFileSetResource(value);}
  return value.kind === "MULTI_DISC" && validMultiDiscResource(value);
}

function validBlobResource(value: Record<string, unknown>) {
  return exactKeys(value, ["kind", "ordinal", "rangeRequired", "role", "sha256", "sizeBytes", "url"]) &&
    validDigest(value.sha256) && positiveInteger(value.sizeBytes) && relativeURL(value.url) &&
    value.rangeRequired === ["SEEKABLE_BLOB", "PARENT_ARCHIVE"].includes(String(value.kind));
}

function validFileTreeResource(value: Record<string, unknown>) {
  return exactKeys(value, ["contentDigest", "indexUrl", "kind", "ordinal", "role"]) &&
    validDigest(value.contentDigest) && relativeURL(value.indexUrl);
}

function validWebResource(value: Record<string, unknown>) {
  return exactKeys(value, [
    "bootstrapTicket", "cleanupUrl", "contentDigest", "entryUrl", "kind", "ordinal", "origin", "role",
  ]) && validDigest(value.contentDigest) && validOrigin(value.origin) &&
    sameOrigin(value.entryUrl, value.origin) && (value.cleanupUrl === null || sameOrigin(value.cleanupUrl, value.origin)) &&
    typeof value.bootstrapTicket === "string" && /^[A-Za-z0-9_-]{43,128}$/u.test(value.bootstrapTicket);
}

function validFileSetResource(value: Record<string, unknown>) {
  return exactKeys(value, ["files", "kind", "ordinal", "role"]) && Array.isArray(value.files) &&
    value.files.length > 0 && value.files.every(validFileEntry) &&
    sortedUnique(value.files.map((entry) => record(entry) ? entry.virtualPath : null));
}

function validMultiDiscResource(value: Record<string, unknown>) {
  return exactKeys(value, ["entries", "initialDiscIndex", "kind", "ordinal", "role"]) &&
    Array.isArray(value.entries) && value.entries.length > 0 &&
    value.entries.every((entry, index) => validDiscEntry(entry, index)) &&
    nonNegativeInteger(value.initialDiscIndex) && value.initialDiscIndex < value.entries.length;
}

function validFileEntry(value: unknown) {
  return record(value) && exactKeys(value, ["logicalName", "sha256", "sizeBytes", "url", "virtualPath"]) &&
    bounded(value.logicalName, 1, 240) && safePath(value.virtualPath) && relativeURL(value.url) &&
    validDigest(value.sha256) && positiveInteger(value.sizeBytes);
}

function validDiscEntry(value: unknown, index: number) {
  return record(value) && exactKeys(value, ["index", "label", "sha256", "sizeBytes", "url"]) &&
    value.index === index && bounded(value.label, 1, 240) && relativeURL(value.url) &&
    validDigest(value.sha256) && positiveInteger(value.sizeBytes);
}

function validTargetOptions(value: unknown): value is TargetOptionsV1 {
  if (!jsonRecord(value)) {return false;}
  try {return new TextEncoder().encode(JSON.stringify(value)).byteLength <= 16 * 1024;}
  catch {return false;}
}

function validRestore(value: unknown, checkpoint: unknown) {
  if (value === null) {return true;}
  return record(checkpoint) && record(value) && exactKeys(value, ["format", "sha256", "sizeBytes", "url"]) &&
    Array.isArray(checkpoint.readFormats) && checkpoint.readFormats.includes(value.format) &&
    validDigest(value.sha256) && positiveInteger(value.sizeBytes) &&
    typeof checkpoint.maxBytes === "number" && value.sizeBytes <= checkpoint.maxBytes && relativeURL(value.url);
}

function validValidation(value: unknown, capabilities: RuntimeCapabilitiesV1) {
  if (value === null) {return true;}
  return record(value) && exactKeys(value, ["input", "probeId"]) &&
    typeof value.probeId === "string" && capabilities.validationProbes.includes(value.probeId) &&
    jsonRecord(value.input);
}

function validNetplay(value: unknown, supported: boolean, session: unknown) {
  if (value === null) {return record(session) && session.mode !== "NETPLAY";}
  return supported && record(session) && session.mode === "NETPLAY" && record(value) && exactKeys(value, [
    "playerNo", "profile", "roomId", "sessionId", "socketUrl",
  ]) && bounded(value.roomId, 1, 128) && uuid(value.sessionId) && positiveInteger(value.playerNo) &&
    value.playerNo <= 16 && webSocketURL(value.socketUrl) && jsonRecord(value.profile);
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
function exactKeys(value: Record<string, unknown>, keys: readonly string[]) {
  const actual = Object.keys(value).sort();
  return actual.length === keys.length && actual.every((key, index) => key === keys[index]);
}
function validIdentity(value: unknown): value is string {return typeof value === "string" && identity.test(value);}
function validToken(value: unknown): value is string {return typeof value === "string" && token.test(value);}
function validDigest(value: unknown): value is string {return typeof value === "string" && digest.test(value);}
function positiveInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0;
}
function nonNegativeInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}
function bounded(value: unknown, minimum: number, maximum: number): value is string {
  return typeof value === "string" && wellFormed(value) &&
    [...value].length >= minimum && [...value].length <= maximum;
}
function uuid(value: unknown): value is string {
  return typeof value === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u.test(value);
}
function relativeURL(value: unknown): value is string {
  return typeof value === "string" && value.length <= 2048 && value.startsWith("/") &&
    !value.startsWith("//") && !value.includes("\\") && !value.includes("#") &&
    [...value].every((character) => character >= " " && character <= "~");
}
function safePath(value: unknown): value is string {
  return typeof value === "string" && value.length >= 1 && value.length <= 240 && !value.startsWith("/") &&
    !value.includes("\\") && !value.includes("?") && !value.includes("#") &&
    value.split("/").every((part) => !["", ".", ".."].includes(part));
}
function validOrigin(value: unknown): value is string {
  if (typeof value !== "string") {return false;}
  try {const url = new URL(value); return ["http:", "https:"].includes(url.protocol) && url.origin === value;}
  catch {return false;}
}
function sameOrigin(value: unknown, origin: string) {
  if (typeof value !== "string") {return false;}
  try {const url = new URL(value); return ["http:", "https:"].includes(url.protocol) && url.origin === origin && !url.hash;}
  catch {return false;}
}
function webSocketURL(value: unknown) {
  if (typeof value !== "string" || value.length > 2048) {return false;}
  try {const url = new URL(value); return ["ws:", "wss:"].includes(url.protocol) && !url.hash;}
  catch {return false;}
}
function sortedUnique(values: unknown[]) {
  return values.every((value) => typeof value === "string") && new Set(values).size === values.length &&
    values.every((value, index) => index === 0 || String(values[index - 1]) < String(value));
}
function jsonRecord(value: unknown): value is Record<string, unknown> {
  return record(value) && Object.keys(value).length <= 64 && Object.keys(value).every(wellFormed) && jsonValue(value, 0);
}
function jsonValue(value: unknown, depth: number): boolean {
  if (depth > 8) {return false;}
  if (value === null || typeof value === "boolean") {return true;}
  if (typeof value === "string") {return wellFormed(value);}
  if (typeof value === "number") {return Number.isSafeInteger(value);}
  if (Array.isArray(value)) {return value.length <= 256 && value.every((entry) => jsonValue(entry, depth + 1));}
  return record(value) && Object.keys(value).length <= 64 && Object.keys(value).every(wellFormed) &&
    Object.values(value).every((entry) => jsonValue(entry, depth + 1));
}
function wellFormed(value: string) {
  for (let index = 0; index < value.length; index += 1) {
    const unit = value.charCodeAt(index);
    if (unit >= 0xd800 && unit <= 0xdbff) {
      const next = value.charCodeAt(index + 1);
      if (next < 0xdc00 || next > 0xdfff) {return false;}
      index += 1;
    } else if (unit >= 0xdc00 && unit <= 0xdfff) {return false;}
  }
  return true;
}
function invalid(): never {throw playerRuntimeError("PLAYER_LAUNCH_ENVELOPE_INVALID");}
