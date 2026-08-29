import {
  rpgMakerPositionProbeKind,
  type CheckpointBlocker,
  type GameRuntime,
} from "@xxxsen/retrom-runtime";

import type { EmulatorInstance } from "./adapters/ejs-4.2.3-v2";
import type { components } from "@/lib/api/generated/schema";

type PayloadKind = components["schemas"]["CheckpointPayloadKind"];
type UnavailableReason = components["schemas"]["CheckpointUnavailableReason"];

type PlayerBridgeOptions = {
  checkpointFormat: string;
  payloadKind: PayloadKind;
  rpgPositionProbe?: boolean;
  validationPurpose?: boolean;
};

export function retromRuntimePlayerInstance(
  runtime: GameRuntime,
  target: HTMLElement,
  options: PlayerBridgeOptions,
): EmulatorInstance {
  const capabilities = runtime.getCapabilities();
  const instance: EmulatorInstance = {
    paused: false,
    on: () => undefined,
    takeScreenshot: async () => ({ blob: await runtime.screenshot(), format: "png" }),
    gameManager: {
      savePayloadKind: options.payloadKind,
      validationPurpose: options.validationPurpose,
      getCheckpointAvailability: () => checkpointAvailability(runtime),
      getStateAsync: async () => checkpointBytes(runtime, options.checkpointFormat),
      getVideoDimensions: (dimension) => videoDimension(runtime, target, dimension),
      toggleMainLoop: (running) => {void (running ? runtime.resume() : runtime.pause());},
    },
  };
  if (capabilities.frameCounter) {
    instance.gameManager!.getFrameNum = () => runtime.getFrameCount() ?? 0;
  }
  if (options.rpgPositionProbe) {
    instance.gameManager!.getRpgPosition = () => rpgPosition(runtime);
  }
  if (capabilities.volume) {
    instance.setVolume = (volume) => runtime.setVolume(volume);
  }
  instance.canvas = runtime.getCanvas() ?? target.querySelector("canvas") ?? undefined;
  return instance;
}

async function checkpointBytes(runtime: GameRuntime, expectedFormat: string) {
  const checkpoint = await runtime.checkpoint();
  if (checkpoint.format !== expectedFormat || !checkpoint.bytes.byteLength) {
    throw new Error("PLAYER_STATE_UNAVAILABLE");
  }
  return checkpoint.bytes;
}

function checkpointAvailability(runtime: GameRuntime) {
  const availability = runtime.getCheckpointAvailability();
  return availability.available
    ? { available: true, reason: null }
    : { available: false, reason: blockerReason(availability.blocker) };
}

function blockerReason(blocker: CheckpointBlocker): UnavailableReason {
  if (blocker === "NOT_READY") {return "RUNTIME_NOT_READY";}
  if (blocker === "BUSY") {return "BUSY";}
  if (blocker === "FAILED") {return "RUNTIME_FAILED";}
  if (blocker === "ALREADY_CREATED") {return "CHECKPOINT_ALREADY_CREATED";}
  if (blocker === "MODE_UNSUPPORTED") {return "NETPLAY_UNSUPPORTED";}
  return "SAVE_DISABLED";
}

function rpgPosition(runtime: GameRuntime) {
  const probe = runtime.getValidationProbe(rpgMakerPositionProbeKind);
  if (!probe || probe.schemaVersion !== 1 || !isRpgPosition(probe.value)) {
    throw new Error("RPG_RUNTIME_POSITION_UNAVAILABLE");
  }
  return probe.value;
}

function isRpgPosition(value: unknown): value is components["schemas"]["RpgPositionEvidence"] {
  if (!value || typeof value !== "object" || Array.isArray(value)) {return false;}
  const position = value as Record<string, unknown>;
  return ["mapId", "playerX", "playerY", "fixtureState"]
    .every((key) => Number.isSafeInteger(position[key]));
}

function videoDimension(
  runtime: GameRuntime,
  target: HTMLElement,
  dimension: "aspect" | "width" | "height",
) {
  const canvas = runtime.getCanvas() ?? target.querySelector("canvas");
  if (!canvas?.width || !canvas.height) {return undefined;}
  if (dimension === "width") {return canvas.width;}
  if (dimension === "height") {return canvas.height;}
  return canvas.width / canvas.height;
}
