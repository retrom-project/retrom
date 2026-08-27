import { SecurityInputBlocked } from "./rpgmaker_security_upload.mjs";

const runtimePaths = new Set(["/__retrom/bootstrap", "/__retrom/entry"]);

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
