import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v1";

export function setEmulatorPaused(instance: EmulatorInstance | undefined, paused: boolean) {
  const manager = instance?.gameManager;
  if (!instance || !manager?.toggleMainLoop) return false;
  manager.toggleMainLoop(!paused);
  instance.paused = paused;
  return true;
}
