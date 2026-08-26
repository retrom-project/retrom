import {
  captureManualScreenshot,
  captureManualState,
  type EmulatorInstance,
  type ManualStatePayload,
} from "./adapters/ejs-4.2.3-v2";

type UploadManualState = (payload: ManualStatePayload) => Promise<boolean>;

export async function saveImmersivePlayerState(
  instance: EmulatorInstance | undefined,
  available: boolean,
  uploadManualState: UploadManualState,
) {
  if (!available || !instance) {return false;}
  try {
    const screenshot = await captureManualScreenshot(instance);
    return await uploadManualState(await captureManualState(instance, screenshot));
  } catch {
    return false;
  }
}
