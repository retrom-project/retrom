import type { components } from "@/lib/api/generated/schema";

export type RpgGeneration = components["schemas"]["RpgGeneration"];
export type RpgCoreId = components["schemas"]["RpgCoreID"];
export type RpgRuntimeConfig = components["schemas"]["RpgMakerLaunchConfig"];
export type CheckpointAvailability = components["schemas"]["CheckpointAvailability"];
export type CheckpointPayloadKind = components["schemas"]["CheckpointPayloadKind"];
export type RpgPosition = components["schemas"]["RpgPositionEvidence"];

export type RuntimeState =
  | "CREATED"
  | "LOADING"
  | "RUNNING"
  | "PAUSED"
  | "CHECKPOINTING"
  | "EXITING"
  | "EXITED"
  | "FAILED";

export type CheckpointPayload = {
  bytes: Uint8Array;
  payloadKind: CheckpointPayloadKind;
};

export type RuntimeEvent =
  | { type: "READY" }
  | { type: "LOAD_PROGRESS"; loadedBytes: number; totalBytes: number | null }
  | { type: "STATE_CHANGED"; previous: RuntimeState; state: RuntimeState }
  | { type: "CHECKPOINT_AVAILABILITY_CHANGED"; availability: CheckpointAvailability }
  | { type: "FATAL_ERROR"; code: string }
  | { type: "EXIT_REQUESTED" };

export interface RetromRpgRuntime {
  mount(target: HTMLElement): Promise<void>;
  pause(): Promise<void>;
  resume(): Promise<void>;
  checkpoint(): Promise<CheckpointPayload>;
  screenshot(): Promise<Blob>;
  exit(): Promise<void>;
  getState(): RuntimeState;
  getCheckpointAvailability(): CheckpointAvailability;
  subscribe(listener: (event: RuntimeEvent) => void): () => void;
}
