import { playerFrameSource } from "./rpg-runtime-csp";

export async function loadPlayerFrameSource(backend: string): Promise<string> {
  const response = await fetch(`${backend}/api/v1/web-config`, {
    cache: "no-store", redirect: "error", signal: AbortSignal.timeout(10_000),
    headers: { Accept: "application/json" }
  });
  if (!response.ok) {throw new Error("WEB_CONFIG_UNAVAILABLE");}
  const value: unknown = await response.json();
  if (!value || typeof value !== "object" || !("runtimeOriginTemplate" in value) ||
    typeof value.runtimeOriginTemplate !== "string" || Object.keys(value).length !== 1) {
    throw new Error("WEB_CONFIG_INVALID");
  }
  const source = playerFrameSource(value.runtimeOriginTemplate);
  if (!source) {throw new Error("WEB_CONFIG_INVALID");}
  return source;
}
