import {
  captureManualScreenshot,
  captureManualState,
  type EmulatorInstance,
  type ManualScreenshot,
} from "./adapters/ejs-4.2.3-v2";

type UploadManualState = (payload: {
  screenshot: Blob;
  format: string;
  state: Uint8Array;
}) => Promise<boolean>;

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
    return await uploadManualState(captureManualState(instance, screenshot));
  } catch {
    return false;
  }
}
