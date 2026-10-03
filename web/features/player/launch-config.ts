import { parseLaunchEnvelopeJSON } from "./runtime/envelope";

export function launchConfigFailure(status: number): string {
  if (status === 401 || status === 403) {return "PLAYER_LAUNCH_SESSION_EXPIRED";}
  if (status === 404 || status === 410) {return "PLAYER_LAUNCH_UNAVAILABLE";}
  if (status === 429 || status >= 500) {return "PLAYER_LAUNCH_SERVICE_UNAVAILABLE";}
  return "PLAYER_LAUNCH_CONFIG_INVALID";
}

export async function readLaunchConfig(launchId: string, signal: AbortSignal) {
  let response: Response;
  try {
    response = await fetch(`/runtime/launches/${launchId}/config`, {
      credentials: "same-origin", cache: "no-store", signal: AbortSignal.any([signal, AbortSignal.timeout(30_000)]),
    });
  } catch (error) {
    if (signal.aborted) {throw error;}
    throw new Error("PLAYER_LAUNCH_NETWORK_FAILED", { cause: error });
  }
  if (!response.ok) {throw new Error(launchConfigFailure(response.status));}
  try {return parseLaunchEnvelopeJSON(await response.text());}
  catch (error) {throw new Error("PLAYER_LAUNCH_CONFIG_INVALID", { cause: error });}
}
