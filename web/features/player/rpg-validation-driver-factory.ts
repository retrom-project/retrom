import { RpgRuntimeValidationDriver } from "./rpg-runtime-validation";
import type { ValidationCheckpointReceipt } from "./rpg-validation-checkpoint-response";
import type { ManualStatePayload } from "./adapters/ejs-4.2.3-v2";
import type { RpgRuntimeConfig } from "./rpg-runtime";

type Options = {
  config: RpgRuntimeConfig;
  signal: AbortSignal;
  uploadCheckpoint: (payload: ManualStatePayload) => Promise<ValidationCheckpointReceipt>;
  finishOriginalLaunch: () => Promise<void>;
};

export function createRpgRuntimeValidationDriver(options: Options) {
  if (options.config.purpose !== "RPG_RUNTIME_VALIDATION") {return null;}
  return new RpgRuntimeValidationDriver({
    config: options.config,
    signal: options.signal,
    uploadCheckpoint: options.uploadCheckpoint,
    finishOriginalLaunch: options.finishOriginalLaunch,
  });
}
