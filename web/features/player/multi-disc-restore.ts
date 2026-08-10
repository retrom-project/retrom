import { switchDisc, type DiscSet, type DiscState, type EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import { injectPersistentSave, type PersistentSaveFileSystem } from "./persistent-save-restore";

export type MultiDiscPersistentRestore = {
  fileSystem: PersistentSaveFileSystem;
  savePath: string;
  bytes: Uint8Array | null;
};

export function restoreMultiDiscLaunch(
  instance: EmulatorInstance,
  discSet: DiscSet,
  persistent: MultiDiscPersistentRestore | null,
  stateBytes: Uint8Array | null,
): DiscState {
  const manager = instance.gameManager;
  if (!manager?.toggleMainLoop) throw new Error("PLAYER_DISC_API_UNAVAILABLE");
  manager.toggleMainLoop(false);
  if (persistent) {
    injectPersistentSave(manager, persistent.fileSystem, persistent.savePath, persistent.bytes);
  }
  const selected = switchDisc(instance, discSet.initialDiscIndex, discSet.count);
  if (stateBytes) {
    if (!manager.loadState) throw new Error("PLAYER_SAVE_STATE_UNAVAILABLE");
    manager.loadState(stateBytes);
  }
  manager.toggleMainLoop(true);
  return selected;
}
