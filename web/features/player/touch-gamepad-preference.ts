const prefix = "retrom:touch-gamepad:";

export function readTouchGamepadPreference(coreId: string): boolean {
  try {return window.localStorage.getItem(`${prefix}${coreId}`) !== "off";}
  catch {return true;}
}

export function writeTouchGamepadPreference(coreId: string, enabled: boolean): void {
  try {window.localStorage.setItem(`${prefix}${coreId}`, enabled ? "on" : "off");}
  catch { /* The current runtime still keeps the choice when browser storage is unavailable. */ }
}
