import type {
  PlayerRuntimeV1,
  RuntimeCheckpointV1,
  RuntimeDiscStateV1,
  RuntimeVideoModeV1,
} from "./contract";

export type RuntimeSavePayload = {
  checkpoint: RuntimeCheckpointV1;
  screenshot: Blob;
};

export async function captureRuntimeSave(runtime: PlayerRuntimeV1): Promise<RuntimeSavePayload> {
  const [checkpoint, screenshot] = await Promise.all([runtime.checkpoint(), runtime.screenshot()]);
  if (!(checkpoint.bytes instanceof Uint8Array) || checkpoint.bytes.byteLength < 1 ||
      typeof checkpoint.format !== "string" || checkpoint.format.length < 1 ||
      !(screenshot instanceof Blob) || screenshot.size < 1) {
    throw new Error("PLAYER_RUNTIME_CONTRACT_INVALID");
  }
  return {
    checkpoint: {...checkpoint, bytes: new Uint8Array(checkpoint.bytes)},
    screenshot,
  };
}

export function setRuntimePaused(runtime: PlayerRuntimeV1, paused: boolean) {
  return paused ? runtime.pause() : runtime.resume();
}

export async function switchRuntimeDisc(runtime: PlayerRuntimeV1, index: number): Promise<RuntimeDiscStateV1> {
  if (!Number.isSafeInteger(index) || index < 0) {throw new Error("PLAYER_RUNTIME_CONTRACT_INVALID");}
  return runtime.switchDisc(index);
}

export function setRuntimeVolume(runtime: PlayerRuntimeV1, value: number) {
  if (!Number.isFinite(value) || value < 0 || value > 1) {throw new Error("PLAYER_RUNTIME_CONTRACT_INVALID");}
  return runtime.setVolume(value);
}

export function setRuntimeVideoMode(runtime: PlayerRuntimeV1, mode: RuntimeVideoModeV1) {
  return runtime.setVideoMode(mode);
}
