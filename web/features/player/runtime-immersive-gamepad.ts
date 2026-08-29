import { installImmersiveGamepadFilter, type ImmersiveGamepadFilter } from "./immersive-gamepad-filter";

export function installRuntimeImmersiveGamepadFilter(
  experience: "standard" | "immersive",
  runtimeWindow: Window,
  filter: ImmersiveGamepadFilter | undefined,
) {
  if (experience !== "immersive") {return undefined;}
  if (!filter) {throw new Error("PLAYER_IMMERSIVE_GAMEPAD_FILTER_UNAVAILABLE");}
  return installImmersiveGamepadFilter(runtimeWindow, filter);
}
