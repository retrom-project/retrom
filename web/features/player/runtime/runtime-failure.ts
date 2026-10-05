import type {RuntimeFailureV1} from "./contract";

/** Validate the public value before retaining it beyond Provider teardown. */
export function readRuntimeFailure(value: RuntimeFailureV1): RuntimeFailureV1 {
  if (!value || !/^[A-Z][A-Z0-9_]{1,127}$/u.test(value.code) ||
    !["STARTUP", "PLAYING"].includes(value.phase) ||
    !["CONTENT", "NETWORK", "STORAGE", "SECURITY", "CORE", "CONFIGURATION"].includes(value.category) ||
    typeof value.retryable !== "boolean" || !Array.isArray(value.diagnostics) || value.diagnostics.length > 8 ||
    value.diagnostics.some(item => !item || !["CORE", "RUNTIME"].includes(item.source) ||
      typeof item.message !== "string" || item.message.length > 500)) {
    return {code: "PLAYER_RUNTIME_CONTRACT_INVALID", category: "CONFIGURATION", phase: "STARTUP", retryable: false, diagnostics: []};
  }
  return {...value, diagnostics: value.diagnostics.map(item => ({...item}))};
}
