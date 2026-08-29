import {
  captureManualScreenshot,
  captureManualState,
  type EmulatorInstance,
  type ManualStatePayload,
  type ManualScreenshot,
} from "./adapters/ejs-4.2.3-v2";

type UploadManualState = (payload: ManualStatePayload) => Promise<boolean>;

export async function saveImmersivePlayerState(
  instance: EmulatorInstance | undefined,
  available: boolean,
  uploadManualState: UploadManualState,
  preparedScreenshot?: Promise<ManualScreenshot | null>,
) {
  if (!available || !instance) {return false;}
  try {
    const screenshot = preparedScreenshot === undefined
      ? await captureManualScreenshot(instance)
      : await preparedScreenshot;
    if (!screenshot) {return false;}
    return await uploadManualState(await captureManualState(instance, screenshot));
  } catch {
    return false;
  }
}
