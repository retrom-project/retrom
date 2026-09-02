import {
  createRuntime,
  type GameRuntime,
  type Wasm4RuntimeConfig,
} from "@xxxsen/retrom-runtime";

import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import { retromRuntimePlayerInstance } from "./retrom-runtime-player";
import type { components } from "@/lib/api/generated/schema";

export type WASM4LaunchConfig = components["schemas"]["WASM4LaunchConfig"];

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/u;
const digestPattern = /^[0-9a-f]{64}$/u;
const configKeys = [
  "adapter", "artifactId", "cartSizeBytes", "checkpoint", "contentDigest", "coreId", "coreName",
  "gameTitle", "launchId", "mode", "platformName", "protocolVersion", "purpose", "returnTo",
  "runtimeFamily", "runtimeVersion", "sessionId", "warnings",
] as const;
const adapterKeys = ["adapterId", "adapterKind", "cartUrl", "runtimeBaseUrl"] as const;
const checkpointKeys = ["payloadKind", "payloadUrl"] as const;

export function isWASM4LaunchConfig(value: unknown): value is WASM4LaunchConfig {
  try {validateWASM4LaunchConfig(value); return true;} catch {return false;}
}

export function validateWASM4LaunchConfig(value: unknown): asserts value is WASM4LaunchConfig {
  if (!recordWithKeys(value, configKeys)) {invalidConfig();}
  validateIdentity(value);
  validateContent(value);
  validatePresentation(value);
  validateAdapter(value.adapter, value.runtimeVersion);
  validateCheckpoint(value.checkpoint, value.launchId);
}

function validateIdentity(value: Record<string, unknown>) {
  if (value.runtimeFamily !== "WASM4" || value.protocolVersion !== 1 || value.mode !== "single" ||
    value.purpose !== "PRODUCT" || value.coreId !== "wasm4" || value.platformName !== "WASM-4" ||
    !uuid(value.launchId) || value.sessionId !== value.launchId || !uuid(value.artifactId)) {
    invalidConfig();
  }
}

function validateContent(value: Record<string, unknown>) {
  if (typeof value.contentDigest !== "string" || !digestPattern.test(value.contentDigest) ||
    !Number.isSafeInteger(value.cartSizeBytes) || Number(value.cartSizeBytes) < 1 ||
    Number(value.cartSizeBytes) > 65536 || !boundedString(value.runtimeVersion, 1, 160)) {
    invalidConfig();
  }
}

function validatePresentation(value: Record<string, unknown>) {
  if (!boundedString(value.coreName, 1, 200) || !boundedString(value.gameTitle, 1, 500) ||
    !relativeURL(value.returnTo) || !Array.isArray(value.warnings) || value.warnings.length > 16 ||
    !value.warnings.every((warning) => boundedString(warning, 1, 200))) {
    invalidConfig();
  }
}

function invalidConfig(): never {throw new Error("PLAYER_WASM4_CONFIG_INVALID");}

function validateAdapter(value: unknown, runtimeVersion: unknown) {
  if (!boundedString(runtimeVersion, 1, 160) || !recordWithKeys(value, adapterKeys) ||
    value.adapterKind !== "WASM4_WEB" || value.adapterId !== "wasm4-web" ||
    value.runtimeBaseUrl !== `/runtime/retrom-runtime/${runtimeVersion}/` || !cartURL(value.cartUrl)) {
    invalidConfig();
  }
}

function validateCheckpoint(value: unknown, launchId: unknown) {
  if (value === null) {return;}
  if (!uuid(launchId) || !recordWithKeys(value, checkpointKeys) || value.payloadKind !== "RUNTIME_STATE" ||
    value.payloadUrl !== `/runtime/launches/${launchId}/state`) {
    invalidConfig();
  }
}

function cartURL(value: unknown) {
  return typeof value === "string" &&
    /^\/runtime\/content\/game\/[0-9a-f]{64}\/[A-Za-z0-9%._~!$&'()+,;=@-]+$/u.test(value) &&
    !/%2f|%5c/iu.test(value);
}

function recordWithKeys(value: unknown, keys: readonly string[]): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value) &&
    Object.keys(value).sort().join("\0") === [...keys].sort().join("\0");
}

function uuid(value: unknown): value is string {return typeof value === "string" && uuidPattern.test(value);}
function boundedString(value: unknown, minimum: number, maximum: number): value is string {
  return typeof value === "string" && value.length >= minimum && value.length <= maximum;
}
function relativeURL(value: unknown): value is string {
  return typeof value === "string" && value.startsWith("/") && !value.startsWith("//") &&
    !value.includes("\\") && !value.includes("#");
}

export function createWASM4ProductRuntime(
  config: WASM4LaunchConfig,
  frameWindow: Window,
  restorePayload: Uint8Array | null,
  signal: AbortSignal,
) {
  validateWASM4LaunchConfig(config);
  const runtimeConfig: Wasm4RuntimeConfig = {
    sessionId: config.sessionId,
    contentDigest: config.contentDigest,
    cartSizeBytes: config.cartSizeBytes,
    adapter: config.adapter,
  };
  return createRuntime(runtimeConfig, {frameWindow, restorePayload, signal});
}

export function wasm4PlayerInstance(runtime: GameRuntime, target: HTMLElement): EmulatorInstance {
  return retromRuntimePlayerInstance(runtime, target, {
    checkpointFormat: "wasm4-state-v1", payloadKind: "RUNTIME_STATE",
  });
}
