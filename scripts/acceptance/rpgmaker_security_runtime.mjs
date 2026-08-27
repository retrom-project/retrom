import { SecurityInputBlocked } from "./rpgmaker_security_upload.mjs";

const runtimePaths = new Set(["/__retrom/bootstrap", "/__retrom/entry"]);

export function requireLocalRuntimeSite(applicationOrigin, runtimeOrigin) {
  if (typeof runtimeOrigin !== "string" || !runtimeOrigin) { return; }
  const application = new URL(applicationOrigin);
  const runtime = new URL(runtimeOrigin);
  const localSite = ".rpg.localhost";
  const applicationSharesSite = application.hostname === "rpg.localhost" ||
    application.hostname.endsWith(localSite);
  if (runtime.hostname.endsWith(localSite) && !applicationSharesSite) {
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

export async function runtimeProjectStatus(frame, logicalName) {
  if (typeof logicalName !== "string" || !logicalName) {
    throw new Error("RPG_ACCEPTANCE_SECURITY_RUNTIME_PROJECT_PATH_INVALID");
  }
  const encoded = logicalName.split("/").map((segment) => encodeURIComponent(segment)).join("/");
  return runtimeRequestStatus(frame, `/__retrom/project/${encoded}`, "GET");
}

export async function runtimeRequestStatus(frame, path, method) {
  if (typeof path !== "string" || !path.startsWith("/__retrom/")) {
    throw new Error("RPG_ACCEPTANCE_SECURITY_RUNTIME_PATH_INVALID");
  }
  if (!new Set(["GET", "POST"]).has(method)) {
    throw new Error("RPG_ACCEPTANCE_SECURITY_RUNTIME_METHOD_INVALID");
  }
  return frame.evaluate(async ({ requestPath, requestMethod }) => {
    const response = await fetch(requestPath, {
      method: requestMethod, credentials: "same-origin", redirect: "manual",
    });
    return response.status;
  }, { requestPath: path, requestMethod: method });
}
