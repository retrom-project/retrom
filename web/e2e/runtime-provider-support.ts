import type {Page} from "@playwright/test";
import type {
  LaunchEnvelopeV1, RuntimeResourceV1, RuntimeStateV1,
} from "../features/player/runtime/contract";

export type RuntimeEnvelope = LaunchEnvelopeV1;

export function runtimeResource(envelope: RuntimeEnvelope, role: string) {
  return envelope.resources.find((resource) => resource.role === role) ?? null;
}

export function runtimeResourceURL(resource: RuntimeResourceV1 | null) {
  if (!resource) {return null;}
  if ("url" in resource) {return resource.url;}
  if ("entryUrl" in resource) {return resource.entryUrl;}
  if ("indexUrl" in resource) {return resource.indexUrl;}
  return null;
}

export function runtimeResourceURLs(resource: RuntimeResourceV1 | null): string[] {
  if (!resource) {return [];}
  const single = runtimeResourceURL(resource);
  if (single) {return [single];}
  if ("files" in resource) {return resource.files.map((file) => file.url);}
  if ("entries" in resource) {return resource.entries.map((entry) => entry.url);}
  return [];
}

export function runtimeFrameCount(page: Page) {
  return page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getFrameCount() ?? 0);
}

export function runtimeState(page: Page): Promise<RuntimeStateV1 | null> {
  return page.evaluate(() => window.__RETROM_E2E_RUNTIME_V1__?.getState() ?? null);
}

export function runtimeCheckpoint(page: Page) {
  return page.evaluate(async () => {
    const diagnostics = window.__RETROM_E2E_RUNTIME_V1__;
    if (!diagnostics) {throw new Error("RUNTIME_E2E_DIAGNOSTICS_UNAVAILABLE");}
    return diagnostics.checkpoint();
  });
}
