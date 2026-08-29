import type { GameRuntime, GameRuntimeEvent, RuntimeState as LibraryRuntimeState } from "@xxxsen/retrom-runtime";
import type { components } from "@/lib/api/generated/schema";

export type RpgGeneration = components["schemas"]["RpgGeneration"];
export type RpgCoreId = components["schemas"]["RpgCoreID"];
export type RpgRuntimeConfig = components["schemas"]["RpgMakerLaunchConfig"];
export type CheckpointAvailability = components["schemas"]["CheckpointAvailability"];
export type CheckpointPayloadKind = components["schemas"]["CheckpointPayloadKind"];
export type RpgPosition = components["schemas"]["RpgPositionEvidence"];

export type RuntimeState = LibraryRuntimeState;

export type CheckpointPayload = {
  bytes: Uint8Array;
  payloadKind: CheckpointPayloadKind;
};

export type RuntimeEvent = GameRuntimeEvent;

export type RetromRpgRuntime = GameRuntime;
