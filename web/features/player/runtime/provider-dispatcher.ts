import type {LaunchEnvelopeV1, PlayerRuntimeV1, ProviderModuleV1, RuntimeHostV1} from "./contract";
import {validateLaunchEnvelopeBoundary} from "./envelope";

export type ProviderImporter = (url: string) => Promise<unknown>;
type DispatcherEnvironment = {crossOriginIsolated: boolean};

export async function loadProviderRuntime(
  envelopeValue: unknown,
  host: RuntimeHostV1,
  importer: ProviderImporter = importProviderModule,
  environment: DispatcherEnvironment = {crossOriginIsolated: globalThis.crossOriginIsolated === true},
): Promise<PlayerRuntimeV1> {
  const envelope = validateLaunchEnvelopeBoundary(envelopeValue);
  if (envelope.runtime.capabilities.requiresThreads && !environment.crossOriginIsolated) {
    throw new Error("PLAYER_RUNTIME_THREADS_UNAVAILABLE");
  }
  const imported = await importer(envelope.runtime.moduleUrl);
  const provider = validateProviderModule(imported, envelope);
  const validated = provider.validateLaunchRequest(envelope);
  if (validated !== envelope) {throw invalidModule();}
  const runtime = await provider.createRuntime(envelope, host);
  validatePlayerRuntime(runtime, envelope);
  return runtime;
}

function validateProviderModule(value: unknown, envelope: LaunchEnvelopeV1): ProviderModuleV1 {
  if (!record(value) || !exactKeys(value, [
    "createRuntime", "providerApiVersion", "providerId", "providerVersion", "validateLaunchRequest",
  ]) || value.providerId !== envelope.runtime.providerId ||
    value.providerVersion !== envelope.runtime.providerVersion ||
    value.providerApiVersion !== envelope.runtime.providerApiVersion ||
    typeof value.validateLaunchRequest !== "function" || typeof value.createRuntime !== "function") {
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
  return import(/* @vite-ignore */ url);
}

function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function exactKeys(value: Record<string, unknown>, expected: string[]) {
  const keys = Object.keys(value).sort();
  return keys.length === expected.length && keys.every((key, index) => key === expected[index]);
}

function invalidModule() {return new Error("PLAYER_PROVIDER_MODULE_INVALID");}
