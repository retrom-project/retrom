import type { PlayerRuntimeV1, RuntimeCheckpointAvailabilityV1 } from "./contract";

/** BUSY may be game-native IO that is independent of a Host checkpoint request. */
export async function waitForNativeSave(runtime: PlayerRuntimeV1, signal?: AbortSignal): Promise<RuntimeCheckpointAvailabilityV1> {
  const deadline = performance.now() + 10_000;
  for (;;) {
    signal?.throwIfAborted();
    const availability = runtime.getCheckpointAvailability();
    if (availability.reason === "FAILED" && runtime.getState() !== "FAILED") { throw new Error("无法确认游戏内存档，请重试同步。"); }
    if (availability.reason !== "BUSY") { return availability; }
    if (performance.now() >= deadline) { throw new Error("游戏内存档仍在写入，请稍后重试同步。"); }
    await nextCheck(signal);
  }
}

function nextCheck(signal?: AbortSignal) {
  return new Promise<void>((resolve) => {
    function finish() { clearTimeout(timer); signal?.removeEventListener("abort", finish); resolve(); }
    const timer = setTimeout(finish, 50);
    signal?.addEventListener("abort", finish, { once: true });
    if (signal?.aborted) { finish(); }
  });
}
