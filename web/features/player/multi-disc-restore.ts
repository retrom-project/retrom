import { switchDisc, type DiscSet, type DiscState, type EmulatorInstance } from "./adapters/ejs-4.2.3-v2";

export function prepareMultiDiscLaunch(
  instance: EmulatorInstance,
  discSet: DiscSet,
): DiscState {
  const manager = instance.gameManager;
  if (!manager?.toggleMainLoop) {throw new Error("PLAYER_DISC_API_UNAVAILABLE");}
  manager.toggleMainLoop(false);
  return switchDisc(instance, discSet.initialDiscIndex, discSet.count);
}
