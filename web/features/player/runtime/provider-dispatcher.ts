import type {LaunchEnvelopeV1, PlayerRuntimeV1, ProviderModuleV1, RuntimeHostV1} from "./contract";
import {validateLaunchEnvelopeBoundary} from "./envelope";
import {PlayerRuntimeError, playerRuntimeError} from "./errors";

export type ProviderImporter = (url: string) => Promise<unknown>;
export type DispatcherEnvironment = {
  createModuleUrl(blob: Blob): string;
  crossOriginIsolated: boolean;
  fetcher: typeof fetch;
  revokeModuleUrl(url: string): void;
  sha256(bytes: Uint8Array): Promise<string>;
};

const maximumModuleBytes = 8 * 1024 * 1024;

export async function loadProviderRuntime(
  envelopeValue: unknown,
  host: RuntimeHostV1,
  importer: ProviderImporter = importProviderModule,
  environment: Partial<DispatcherEnvironment> = {},
): Promise<PlayerRuntimeV1> {
  const envelope = validateLaunchEnvelopeBoundary(envelopeValue);
  const dispatcher = dispatcherEnvironment(environment);
  if (envelope.runtime.capabilities.requiresThreads && !dispatcher.crossOriginIsolated) {
    throw playerRuntimeError("PLAYER_RUNTIME_THREADS_UNAVAILABLE");
  }
  const moduleBytes = await fetchProviderModule(envelope, host, dispatcher);
  const moduleUrl = dispatcher.createModuleUrl(new Blob([moduleBytes], {
    type: "text/javascript; charset=utf-8",
  }));
  let imported: unknown;
  try {
    imported = await importer(moduleUrl);
  } catch {
    throw invalidModule();
  } finally {
    dispatcher.revokeModuleUrl(moduleUrl);
  }
  const provider = validateProviderModule(imported, envelope);
  const runtime = await provider.createRuntime(envelope, host);
  validatePlayerRuntime(runtime, envelope);
  return runtime;
}

function dispatcherEnvironment(overrides: Partial<DispatcherEnvironment>): DispatcherEnvironment {
  return {
    createModuleUrl: overrides.createModuleUrl ?? ((blob) => URL.createObjectURL(blob)),
    crossOriginIsolated: overrides.crossOriginIsolated ?? globalThis.crossOriginIsolated === true,
    fetcher: overrides.fetcher ?? ((input, init) => globalThis.fetch(input, init)),
    revokeModuleUrl: overrides.revokeModuleUrl ?? ((url) => URL.revokeObjectURL(url)),
    sha256: overrides.sha256 ?? digestSha256,
  };
}

async function fetchProviderModule(
  envelope: LaunchEnvelopeV1,
  host: RuntimeHostV1,
  environment: DispatcherEnvironment,
) {
  try {
    const response = await environment.fetcher(envelope.runtime.moduleUrl, {
      cache: "no-store", credentials: "same-origin", redirect: "error", signal: host.signal,
    });
    if (!response.ok || response.headers.get("content-type") !== "text/javascript; charset=utf-8") {
      throw invalidModule();
    }
    const declaredLength = response.headers.get("content-length");
    if (declaredLength !== null && (!/^(0|[1-9][0-9]*)$/u.test(declaredLength) ||
      Number(declaredLength) < 1 || Number(declaredLength) > maximumModuleBytes)) {
      throw invalidModule();
    }
    const bytes = await readBoundedBody(response, maximumModuleBytes);
    if (bytes.byteLength < 1 || declaredLength !== null && bytes.byteLength !== Number(declaredLength)) {
      throw invalidModule();
    }
    if (await environment.sha256(bytes) !== envelope.runtime.moduleSha256) {
      throw playerRuntimeError("PLAYER_PROVIDER_MODULE_DIGEST_INVALID");
    }
    return bytes;
  } catch (error) {
    if (error instanceof PlayerRuntimeError) {throw error;}
    throw invalidModule();
  }
}

async function readBoundedBody(response: Response, maximumBytes: number) {
  if (!response.body) {throw invalidModule();}
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let size = 0;
  try {
    while (true) {
      const {done, value} = await reader.read();
      if (done) {break;}
      size += value.byteLength;
      if (size > maximumBytes) {throw invalidModule();}
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  const bytes = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {bytes.set(chunk, offset); offset += chunk.byteLength;}
  return bytes;
}

async function digestSha256(bytes: Uint8Array) {
  const copy = Uint8Array.from(bytes);
  const digest = await crypto.subtle.digest("SHA-256", copy.buffer);
  return [...new Uint8Array(digest)].map((value) => value.toString(16).padStart(2, "0")).join("");
}

function validateProviderModule(value: unknown, envelope: LaunchEnvelopeV1): ProviderModuleV1 {
  if (!record(value) || !exactKeys(value, [
    "createRuntime", "providerApiVersion", "providerId", "providerVersion",
  ]) || value.providerId !== envelope.runtime.providerId ||
    value.providerVersion !== envelope.runtime.providerVersion ||
    value.providerApiVersion !== envelope.runtime.providerApiVersion ||
    typeof value.createRuntime !== "function") {
    throw invalidModule();
  }
  return value as unknown as ProviderModuleV1;
}

function validatePlayerRuntime(value: unknown, envelope: LaunchEnvelopeV1): asserts value is PlayerRuntimeV1 {
  if (!record(value)) {throw invalidModule();}
  for (const method of [
    "mount", "pause", "resume", "checkpoint", "screenshot", "exit", "getState", "getCapabilities",
    "getCheckpointAvailability", "getCanvas", "getFrameCount", "setVolume", "setVideoMode",
    "openNativeSettings", "closeNativeSettings", "getDiscState", "switchDisc", "setInputFilter",
    "getNetplayPort", "runValidationProbe", "subscribe",
  ]) {
    if (typeof value[method] !== "function") {throw invalidModule();}
  }
  const runtime = value as unknown as PlayerRuntimeV1;
  if (runtime.getState() !== "CREATED" ||
    !capabilitiesEqual(runtime.getCapabilities(), envelope.runtime.capabilities)) {throw invalidModule();}
}

function capabilitiesEqual(
  actual: ReturnType<PlayerRuntimeV1["getCapabilities"]>,
  expected: LaunchEnvelopeV1["runtime"]["capabilities"],
) {
  if (!record(actual) || !exactKeys(actual, [
    "checkpoint", "discSwitch", "frameCounter", "frameMode", "inputFilter", "nativeSettings", "netplayPort",
    "pause", "requiresThreads", "screenshot", "standardGamepad", "validationProbes", "videoModes", "volume",
  ])) {return false;}
  const scalarKeys = [
    "checkpoint", "discSwitch", "frameCounter", "frameMode", "inputFilter", "nativeSettings", "netplayPort",
    "pause", "requiresThreads", "screenshot", "standardGamepad", "volume",
  ] as const;
  return scalarKeys.every((key) => actual[key] === expected[key]) &&
    stringArraysEqual(actual.validationProbes, expected.validationProbes) &&
    stringArraysEqual(actual.videoModes, expected.videoModes);
}

function stringArraysEqual(actual: unknown, expected: readonly string[]) {
  return Array.isArray(actual) && actual.length === expected.length &&
    actual.every((value, index) => value === expected[index]);
}

async function importProviderModule(url: string): Promise<unknown> {
  return import(/* webpackIgnore: true */ /* turbopackIgnore: true */ url);
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function exactKeys(value: Record<string, unknown>, expected: string[]) {
  const keys = Object.keys(value).sort();
  return keys.length === expected.length && keys.every((key, index) => key === expected[index]);
}

function invalidModule() {return playerRuntimeError("PLAYER_PROVIDER_MODULE_INVALID");}
