import type {RuntimeTouchStickModeV1} from "./runtime/contract";

const prefix = "retrom:touch-stick:";

/** Device-local stick behaviour per core; floating unless the player chose a fixed stick. */
export function readTouchStickPreference(coreId: string): RuntimeTouchStickModeV1 {
  try {return window.localStorage.getItem(`${prefix}${coreId}`) === "FIXED" ? "FIXED" : "FLOATING";}
  catch {return "FLOATING";}
}

export function writeTouchStickPreference(coreId: string, mode: RuntimeTouchStickModeV1): void {
  try {window.localStorage.setItem(`${prefix}${coreId}`, mode);}
  catch { /* Without browser storage the choice cannot outlive this page; the next launch keeps floating. */ }
}
