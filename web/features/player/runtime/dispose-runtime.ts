import type { PlayerRuntimeV1, RuntimeCheckpointV1, RuntimeFinalSnapshotV1 } from "./contract";
import { waitForNativeSave } from "./wait-native-save";
export async function disposeRuntime(
  runtime: PlayerRuntimeV1,
  onSnapshot: (snapshot: RuntimeFinalSnapshotV1) => Promise<void>,
  beforeSnapshot: () => Promise<void> = async () => {},
) {
  try {
    await beforeSnapshot();
    if (runtime.getState() === "CREATED") { return; }
    const availability = await waitForNativeSave(runtime);
    if (
      availability.available &&
      availability.revision
    ) {
      const checkpoint = await runtime.checkpoint({ intent: "EXPORT" });
      const screenshot = await checkpointScreenshot(runtime, checkpoint);
      await onSnapshot({ checkpoint, screenshot });
    }
  } finally {
    await runtime.exit();
  }
}

/** A null screenshot keeps the save's current image; the runtime marks exports it does not show. */
export function checkpointScreenshot(runtime: PlayerRuntimeV1, checkpoint: RuntimeCheckpointV1, keepsImage = true) {
  // A Runtime image can come from another realm, so check the declared union.
  if (checkpoint.screenshot && checkpoint.screenshot !== "UNCHANGED") { return Promise.resolve(checkpoint.screenshot); }
  return checkpoint.screenshot === "UNCHANGED" && keepsImage ? Promise.resolve(null) : runtime.screenshot().catch(() => null);
}
