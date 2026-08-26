import type {
  CheckpointAvailability,
  CheckpointPayload,
  CheckpointPayloadKind,
  RpgPosition,
} from "./contract";

export type RpgPlayerInstance = {
  canvas?: HTMLCanvasElement;
  paused?: boolean;
  volume?: number;
  muted?: boolean;
  setVolume?: (volume: number) => void;
  on: (event: string, callback: (...args: unknown[]) => void) => void;
  takeScreenshot?: (source?: string, format?: string, upscale?: number) => Promise<{ blob: Blob; format: string }>;
  gameManager?: {
    savePayloadKind?: CheckpointPayloadKind;
    validationPurpose?: boolean;
    getRpgPosition?: () => RpgPosition;
    getCheckpointAvailability?: () => CheckpointAvailability;
    getStateAsync?: () => Promise<Uint8Array>;
    getFrameNum?: () => number;
    getVideoDimensions?: (dimension: "aspect" | "width" | "height") => number | undefined;
    loadExplicitStateAndWait?: (bytes: Uint8Array, timeoutMs?: number) => Promise<void>;
    toggleMainLoop?: (running: boolean) => void | Promise<void>;
  };
};

export type MountedRpgAdapter = {
  cleanup: () => void | Promise<void>;
  instance: RpgPlayerInstance;
};

export type RpgRuntimeAdapter = {
  checkpoint(): Promise<CheckpointPayload>;
  exit(): Promise<void>;
  getCanvas(): HTMLCanvasElement | undefined;
  getCheckpointAvailability(): CheckpointAvailability;
  getFrameCount(): number;
  getPayloadKind(): CheckpointPayloadKind;
  getPosition(): RpgPosition;
  pause(): Promise<void>;
  resume(): Promise<void>;
  screenshot(): Promise<Blob>;
  setVolume(value: number): void;
};

export function adaptMountedRpgAdapter(mounted: MountedRpgAdapter): RpgRuntimeAdapter {
  const manager = mounted.instance.gameManager;
  if (!manager?.getStateAsync || !manager.getCheckpointAvailability || !manager.getRpgPosition ||
    !manager.getFrameNum || !manager.toggleMainLoop || !mounted.instance.takeScreenshot ||
    !manager.savePayloadKind) {
    throw new Error("RPG_RUNTIME_ADAPTER_INVALID");
  }
  const checkpoint = manager.getStateAsync.bind(manager);
  const checkpointAvailability = manager.getCheckpointAvailability.bind(manager);
  const frameCount = manager.getFrameNum.bind(manager);
  const payloadKind = manager.savePayloadKind;
  const position = manager.getRpgPosition.bind(manager);
  const screenshot = mounted.instance.takeScreenshot.bind(mounted.instance);
  const toggleMainLoop = manager.toggleMainLoop.bind(manager);
  return {
    checkpoint: async () => ({ bytes: await checkpoint(), payloadKind }),
    exit: async () => {await mounted.cleanup();},
    getCanvas: () => mounted.instance.canvas,
    getCheckpointAvailability: () => checkpointAvailability(),
    getFrameCount: () => frameCount(),
    getPayloadKind: () => payloadKind,
    getPosition: () => position(),
    pause: async () => {await toggleMainLoop(false);},
    resume: async () => {await toggleMainLoop(true);},
    screenshot: async () => (await screenshot()).blob,
    setVolume: (value) => {
      mounted.instance.volume = value;
      mounted.instance.muted = value === 0;
      mounted.instance.setVolume?.(value);
    },
  };
}
