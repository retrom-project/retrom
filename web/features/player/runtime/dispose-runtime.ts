import type { PlayerRuntimeV1, RuntimeFinalSnapshotV1 } from "./contract";
export async function disposeRuntime(
  runtime: PlayerRuntimeV1,
  onSnapshot: (snapshot: RuntimeFinalSnapshotV1) => Promise<void>,
) {
  try {
    const availability = runtime.getCheckpointAvailability();
    if (
      runtime.getState() !== "CREATED" &&
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
