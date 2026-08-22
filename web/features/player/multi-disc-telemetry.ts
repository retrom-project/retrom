import type { components } from "@/lib/api/generated/schema";

export type MultiDiscPlayerEvent = components["schemas"]["MultiDiscPlayerEventRequest"];
export type MultiDiscPlayerResultCode = MultiDiscPlayerEvent["resultCode"];

function isMultiDiscPlayerResultCode(value: string): value is MultiDiscPlayerResultCode {
  return value === "OK" || value === "PLAYER_DISC_SET_INVALID" || value === "PLAYER_DISC_API_UNAVAILABLE"
    || value === "PLAYER_DISC_SWITCH_UNAVAILABLE" || value === "PLAYER_DISC_SWITCH_FAILED"
    || value === "PLAYER_SAVE_STATE_UNAVAILABLE" || value === "PLAYER_SAVE_STATE_RESTORE_FAILED"
    || value === "LAUNCH_PERSISTENT_SAVE_LOAD_FAILED";
}

export function multiDiscPlayerResultCode(
  error: unknown,
  fallback: MultiDiscPlayerResultCode,
): MultiDiscPlayerResultCode {
  const code = error instanceof Error ? error.message : "";
  return isMultiDiscPlayerResultCode(code) ? code : fallback;
}

export async function reportMultiDiscPlayerEvent(
  launchId: string,
  event: MultiDiscPlayerEvent,
): Promise<void> {
  const response = await fetch(`/runtime/launches/${encodeURIComponent(launchId)}/player-events`, {
    method: "POST",
    credentials: "same-origin",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(event),
  });
  if (!response.ok) {throw new Error("PLAYER_EVENT_REPORT_FAILED");}
}
