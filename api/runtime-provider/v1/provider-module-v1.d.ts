export type ProviderApiVersionV1 = 1;

export type RuntimePurposeV1 = "PRODUCT" | "REVIEW_PREVIEW";
export type RuntimeModeV1 = "SINGLE" | "NETPLAY";
export type RuntimeStateV1 = "CREATED" | "MOUNTING" | "RUNNING" | "PAUSED" |
  "CHECKPOINTING" | "EXITING" | "EXITED" | "FAILED";
export type RuntimeFrameModeV1 =
  | "NONE"
  | "SAME_ORIGIN_BLANK"
  | "SAME_ORIGIN_RESOURCE"
  | "ISOLATED_ORIGIN_RESOURCE";
export type RuntimeVideoModeV1 =
  | "original"
  | "pixel"
  | "smooth"
  | "sharp-bilinear"
  | "adaptive-sharpen";

export type RuntimeCapabilitiesV1 = {
  pause: boolean;
  screenshot: boolean;
  checkpoint: boolean;
  standardGamepad: boolean;
  frameCounter: boolean;
  volume: boolean;
  discSwitch: boolean;
  nativeSettings: boolean;
  inputFilter: boolean;
  netplayPort: boolean;
  videoModes: RuntimeVideoModeV1[];
  requiresThreads: boolean;
  frameMode: RuntimeFrameModeV1;
};

export type RuntimeCheckpointV1 = {
  format: string;
  bytes: Uint8Array;
  metadata: Record<string, unknown> | null;
};
export type RuntimeCheckpointAvailabilityV1 = {
  available: boolean;
  reason: string | null;
  /** Stable identity of unsynchronized native save content; never changes for identical writes. */
  revision?: string;
};
export type RuntimeDiscStateV1 = { count: number; currentIndex: number; labels: string[] };
export type RuntimeInputFilterPolicyV1 = { activeGamepadIndex: number | null; suppressInput: boolean };

export type RuntimeEventV1 =
  | { type: "STATE_CHANGED"; previous: RuntimeStateV1; state: RuntimeStateV1 }
  | { type: "LOAD_PROGRESS"; loadedBytes: number; totalBytes: number | null }
  | { type: "CHECKPOINT_AVAILABILITY_CHANGED"; availability: RuntimeCheckpointAvailabilityV1 }
  | { type: "DISC_CHANGED"; state: RuntimeDiscStateV1 }
  | { type: "EXIT_REQUESTED" }
  | { type: "FATAL_ERROR"; code: string }
  | { type: "DIAGNOSTIC"; code: string; message: string };

export type RuntimeEventListenerV1 = (event: RuntimeEventV1) => void;

export interface RuntimeNetplayPortV1 {
  readonly controlCount: number;
  pauseAtBoundary(): Promise<number>;
  captureState(frame: number): Promise<Uint8Array>;
  loadStateAndWait(state: Uint8Array, frame: number): Promise<void>;
  runFrame(controls: Int16Array, frame: number, suppressOutput: boolean): Promise<void>;
  sampleLocalControls(): Int16Array;
  resetLocalControls(): void;
  close(): Promise<void>;
}

export interface PlayerRuntimeV1 {
  mount(target: HTMLElement): Promise<void>;
  pause(): Promise<void>;
  resume(): Promise<void>;
  checkpoint(): Promise<RuntimeCheckpointV1>;
  /** GAME_SAVE: acknowledge this exact payload only after durable Host persistence succeeds. */
  acknowledgeCheckpoint?(checkpoint: RuntimeCheckpointV1): Promise<void>;
  screenshot(): Promise<Blob>;
  setVolume(value: number): Promise<void>;
  setVideoMode(mode: RuntimeVideoModeV1): Promise<void>;
  openNativeSettings(panel: "controls" | "display" | "core"): Promise<void>;
  closeNativeSettings(): Promise<void>;
  getDiscState(): Promise<RuntimeDiscStateV1>;
  switchDisc(index: number): Promise<RuntimeDiscStateV1>;
  setInputFilter(policy: RuntimeInputFilterPolicyV1 | null): Promise<void>;
  getNetplayPort(): Promise<RuntimeNetplayPortV1>;
  getState(): RuntimeStateV1;
  getCapabilities(): RuntimeCapabilitiesV1;
  getCheckpointAvailability(): RuntimeCheckpointAvailabilityV1;
  getCanvas(): HTMLCanvasElement | null;
  getFrameCount(): number | null;
  subscribe(listener: RuntimeEventListenerV1): () => void;
  exit(): Promise<void>;
}

export interface RuntimeFrameV1 {
  element: HTMLIFrameElement;
  contentWindow: WindowProxy;
  origin: string;
}

export interface RuntimeHostV1 {
  signal: AbortSignal;
  mountFrame(target: HTMLElement, input: { resourceRole: string | null }): Promise<RuntimeFrameV1>;
  loadRestore(descriptor: RestoreDescriptorV1 | null): Promise<Uint8Array | null>;
  reportDiagnostic(input: { code: string; message: string }): void;
}

export type RestoreDescriptorV1 = { url: string; format: string; sha256: string; sizeBytes: number };
export type RuntimeResourceIdentityV1 = { role: string; ordinal: number };
export type RuntimeBlobResourceV1 = RuntimeResourceIdentityV1 & {
  kind: "ROM_BLOB" | "SEEKABLE_BLOB" | "PARENT_ARCHIVE" | "WASM4_CART";
  url: string;
  sha256: string;
  sizeBytes: number;
  rangeRequired: boolean;
};
export type RuntimeFileTreeResourceV1 = RuntimeResourceIdentityV1 & {
  kind: "FILE_TREE";
  indexUrl: string;
  contentDigest: string;
};
export type RuntimeWebResourceV1 = RuntimeResourceIdentityV1 & {
  kind: "NATIVE_WEB" | "ISOLATED_WEB";
  origin: string;
  entryUrl: string;
  bootstrapTicket: string;
  cleanupUrl: string | null;
  contentDigest: string;
};
export type RuntimeFileEntryV1 = {
  logicalName: string;
  virtualPath: string;
  url: string;
  sha256: string;
  sizeBytes: number;
};
export type RuntimeFileSetResourceV1 = RuntimeResourceIdentityV1 & {
  kind: "BIOS_BUNDLE" | "EXTERNAL_FILE_SET";
  files: RuntimeFileEntryV1[];
};
export type RuntimeMultiDiscResourceV1 = RuntimeResourceIdentityV1 & {
  kind: "MULTI_DISC";
  initialDiscIndex: number;
  entries: Array<{ index: number; label: string; url: string; sha256: string; sizeBytes: number }>;
};
export type RuntimeResourceV1 = RuntimeBlobResourceV1 | RuntimeFileTreeResourceV1 |
  RuntimeWebResourceV1 | RuntimeFileSetResourceV1 | RuntimeMultiDiscResourceV1;

export type RuntimeJSONValueV1 = null | boolean | string | number |
  RuntimeJSONValueV1[] | { [key: string]: RuntimeJSONValueV1 };
export type TargetOptionsV1 = { [key: string]: RuntimeJSONValueV1 };

export type NetplayProfileV2 = {
  bundleSha256: string;
  canonicalHistoryFrames: 600;
  checkpointEveryFrames: 120;
  controlCount: 24;
  coreId: string;
  dependencySnapshotDigest: string;
  maxPlayers: number;
  maxPredictionFrames: number;
  maxRollbackFrames: 120;
  maxStateBytes: 1048576;
  platformIds: string[];
  profileId: string;
  protocolVersion: "retrom-netplay-v2";
  providerId: string;
  schemaVersion: 2;
  sourceManifestDigest: string;
  targetId: string;
};
export type RuntimeNetplayV1 = {
  roomId: string;
  sessionId: string;
  playerNo: number;
  socketUrl: string;
  profile: NetplayProfileV2;
};

export type LaunchEnvelopeV1 = {
  schemaVersion: 1;
  session: {
    id: string;
    purpose: RuntimePurposeV1;
    mode: RuntimeModeV1;
    title: string;
    platformName: string;
    coreName: string;
    returnTo: string;
    warnings: string[];
  };
  runtime: {
    providerId: string;
    providerVersion: string;
    providerApiVersion: 1;
    bundleSha256: string;
    targetId: string;
    capabilities: RuntimeCapabilitiesV1;
    // Omitted semantics means INSTANT; GAME_SAVE may require the game's save/load menu.
    checkpoint: { writeFormat: string; readFormats: string[]; maxBytes: number; semantics?: "INSTANT" | "GAME_SAVE" } | null;
    moduleUrl: string;
    moduleSha256: string;
    runtimeBaseUrl: string;
  };
  resources: RuntimeResourceV1[];
  targetOptions: TargetOptionsV1;
  restore: RestoreDescriptorV1 | null;
  netplay: RuntimeNetplayV1 | null;
};

export type ProviderLaunchRequestV1 = LaunchEnvelopeV1;

export interface ProviderModuleV1 {
  providerId: string;
  providerVersion: string;
  providerApiVersion: 1;
  createRuntime(request: unknown, host: RuntimeHostV1): Promise<PlayerRuntimeV1>;
}
