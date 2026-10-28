import type { PlayerRuntimeV1, RuntimeFinalSnapshotV1 } from "./contract";
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
      const screenshot = await runtime.screenshot().catch(() => null);
      await onSnapshot({ checkpoint, screenshot });
    }
  } finally {
    await runtime.exit();
  }
}
