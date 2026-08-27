import { SecurityInputBlocked } from "./rpgmaker_security_upload.mjs";

const runtimePaths = new Set(["/__retrom/bootstrap", "/__retrom/entry"]);

export function requireLocalRuntimeSite(applicationOrigin, runtimeOrigin) {
  if (typeof runtimeOrigin !== "string" || !runtimeOrigin) { return; }
  const application = new URL(applicationOrigin);
  const runtime = new URL(runtimeOrigin);
  if (runtime.hostname.endsWith(".rpg.localhost") && application.hostname !== "localhost") {
    throw new SecurityInputBlocked("RPG_ACCEPTANCE_SECURITY_RUNTIME_SITE_MISMATCH");
  }
}

export function runtimeFrameEligible(frameUrl, expectedOrigin) {
  return typeof expectedOrigin !== "string" || !expectedOrigin ||
    runtimeFrameRoute(frameUrl, expectedOrigin) === "RUNTIME";
}

export function runtimeFrameRoute(frameUrl, expectedOrigin) {
  if (frameUrl === "about:blank") { return "WAIT"; }
  let parsed;
  try {
    parsed = new URL(frameUrl);
  } catch {
    return "WAIT";
  }
  if (parsed.origin !== expectedOrigin) { return "WAIT"; }
  if (runtimePaths.has(parsed.pathname)) { return "RUNTIME"; }
  throw new SecurityInputBlocked("RPG_ACCEPTANCE_SECURITY_RUNTIME_ORIGIN_MISROUTED");
}
