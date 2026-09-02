export type ProviderApiVersionV1 = 1;

export type RuntimePurposeV1 = "PRODUCT" | "REVIEW_PREVIEW" | "RUNTIME_VALIDATION";
export type RuntimeModeV1 = "SINGLE" | "NETPLAY";
export type RuntimeStateV1 = "CREATED" | "MOUNTING" | "RUNNING" | "PAUSED" | "EXITED" | "FAILED";
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
  validationProbes: string[];
};

export type RuntimeCheckpointV1 = {
  format: string;
  bytes: Uint8Array;
  metadata: Record<string, unknown> | null;
};
export type RuntimeCheckpointAvailabilityV1 = { available: boolean; reason: string | null };
export type RuntimeDiscStateV1 = { count: number; currentIndex: number; labels: string[] };
export type RuntimeInputFilterPolicyV1 = { activeGamepadIndex: number | null; suppressInput: boolean };
export type RuntimeValidationResultV1 = {
  probeId: string;
  passed: boolean;
  evidence: Record<string, unknown>;
};

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
  screenshot(): Promise<Blob>;
  setVolume(value: number): Promise<void>;
  setVideoMode(mode: RuntimeVideoModeV1): Promise<void>;
  openNativeSettings(panel: "controls" | "display" | "core"): Promise<void>;
  closeNativeSettings(): Promise<void>;
  getDiscState(): Promise<RuntimeDiscStateV1>;
  switchDisc(index: number): Promise<RuntimeDiscStateV1>;
  setInputFilter(policy: RuntimeInputFilterPolicyV1 | null): Promise<void>;
  getNetplayPort(): Promise<RuntimeNetplayPortV1>;
  runValidationProbe(id: string, input: Record<string, unknown>): Promise<RuntimeValidationResultV1>;
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
  kind: "ROM_BLOB_V1" | "SEEKABLE_BLOB_V1" | "PARENT_ARCHIVE_V1" | "WASM4_CART_V1";
  url: string;
  sha256: string;
  sizeBytes: number;
  rangeRequired: boolean;
};
export type RuntimeFileTreeResourceV1 = RuntimeResourceIdentityV1 & {
  kind: "FILE_TREE_V1";
  indexUrl: string;
  contentDigest: string;
};
export type RuntimeWebResourceV1 = RuntimeResourceIdentityV1 & {
  kind: "NATIVE_WEB_V1" | "ISOLATED_WEB_V1";
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
  kind: "BIOS_BUNDLE_V1" | "EXTERNAL_FILE_SET_V1";
  files: RuntimeFileEntryV1[];
};
export type RuntimeMultiDiscResourceV1 = RuntimeResourceIdentityV1 & {
  kind: "MULTI_DISC_V1";
  initialDiscIndex: number;
  entries: Array<{ index: number; label: string; url: string; sha256: string; sizeBytes: number }>;
};
export type RuntimeResourceV1 = RuntimeBlobResourceV1 | RuntimeFileTreeResourceV1 |
  RuntimeWebResourceV1 | RuntimeFileSetResourceV1 | RuntimeMultiDiscResourceV1;

export type TargetOptionsV1 =
  | { kind: "NONE_V1" }
  | { kind: "EMULATORJS_V1"; dosEntryPath: string | null; initialDiscIndex: number | null }
  | { kind: "RPGMAKER_V1"; expectedRestorePosition: {
      mapId: number; playerX: number; playerY: number; fixtureState: number;
    } | null }
  | { kind: "ONS_PROJECT_V1"; scriptEncoding: "gbk" | "sjis" | "utf8" }
  | { kind: "KIRIKIRI_PROJECT_V1"; startupXp3Path: string | null };

export type RuntimeValidationV1 = { probeId: string; input: Record<string, unknown> };
export type RuntimeNetplayV1 = {
  roomId: string;
  sessionId: string;
  playerNo: number;
  socketUrl: string;
  profile: Record<string, unknown>;
};

export type LaunchEnvelopeV1 = {
  schemaVersion: 1;
  session: {
    id: string;
    purpose: RuntimePurposeV1;
    mode: RuntimeModeV1;
    title: string;
    platformName: string;
    returnTo: string;
    warnings: string[];
  };
  runtime: {
    providerId: string;
    providerVersion: string;
    providerApiVersion: 1;
    bundleSha256: string;
    targetId: string;
    gameCompatibilityLine: string;
    targetContractSha256: string;
    capabilities: RuntimeCapabilitiesV1;
    checkpoint: { writeFormat: string; readFormats: string[]; maxBytes: number } | null;
    moduleUrl: string;
    moduleSha256: string;
    runtimeBaseUrl: string;
  };
  resources: RuntimeResourceV1[];
  targetOptions: TargetOptionsV1;
  restore: RestoreDescriptorV1 | null;
  validation: RuntimeValidationV1 | null;
  netplay: RuntimeNetplayV1 | null;
};

export type ProviderLaunchRequestV1 = LaunchEnvelopeV1;

export interface ProviderModuleV1 {
  providerId: string;
  providerVersion: string;
  providerApiVersion: 1;
  validateLaunchRequest(value: unknown): ProviderLaunchRequestV1;
  createRuntime(request: ProviderLaunchRequestV1, host: RuntimeHostV1): Promise<PlayerRuntimeV1>;
}
