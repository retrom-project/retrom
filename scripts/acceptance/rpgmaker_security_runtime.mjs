import { SecurityInputBlocked } from "./rpgmaker_security_upload.mjs";

const runtimePaths = new Set(["/__retrom/bootstrap", "/__retrom/entry"]);

export function requireLocalRuntimeSite(applicationOrigin, runtimeOrigin) {
  if (typeof runtimeOrigin !== "string" || !runtimeOrigin) { return; }
  const application = new URL(applicationOrigin);
  const runtime = new URL(runtimeOrigin);
  const localSite = ".rpg.localhost";
  const applicationSharesSite = application.hostname === "rpg.localhost" ||
    application.hostname.endsWith(localSite);
  const runtimeIsLocal = runtime.hostname === "localhost" || runtime.hostname.endsWith(".localhost");
  const applicationPfb = /^([a-z0-9](?:[a-z0-9-]{0,22}[a-z0-9])?)\.localhost$/.exec(
    application.hostname,
  );
  const runtimePfb = /^([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[89ab][0-9a-f]{3}-[0-9a-f]{12})\.rpg\.([a-z0-9](?:[a-z0-9-]{0,22}[a-z0-9])?)\.localhost$/.exec(
    runtime.hostname,
  );
  const pfbPair = applicationPfb !== null && runtimePfb !== null && applicationPfb[1] === runtimePfb[2];
  if (runtimeIsLocal && !((runtime.hostname.endsWith(localSite) && applicationSharesSite) || pfbPair)) {
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
  const allowedPath = typeof path === "string" &&
    (path.startsWith("/__retrom/") || path === "/api/v1/admin/reviews");
  if (!allowedPath) {
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

export async function runtimeBootstrapReplayStatus(frame, ticket) {
  if (typeof ticket !== "string" || !ticket) {
    throw new Error("RPG_ACCEPTANCE_SECURITY_RUNTIME_TICKET_INVALID");
  }
  return frame.evaluate(async (bootstrapTicket) => {
    const response = await fetch("/__retrom/bootstrap", {
      method: "POST", credentials: "same-origin", redirect: "manual",
      headers: { "Content-Type": "application/json" }, body: JSON.stringify({ ticket: bootstrapTicket }),
    });
    return response.status;
  }, ticket);
}

export async function browserNavigationStatus(context, url) {
  const page = await context.newPage();
  let status = null;
  page.on("response", (response) => {
    if (response.url() === url && status === null) { status = response.status(); }
  });
  try {
    if (new URL(url).pathname === "/__retrom/bootstrap") {
      await page.route("**/__retrom/entry", (route) => route.abort("blockedbyclient"));
    }
    await page.goto(url, { waitUntil: "domcontentloaded", timeout: 120_000 }).catch((error) => {
      if (status === null) { throw error; }
    });
    if (status === null) { throw new Error("RPG_ACCEPTANCE_SECURITY_NAVIGATION_STATUS_MISSING"); }
    return status;
  } finally {
    await page.close();
  }
}

export function confusedRuntimeEntryURL(runtimeOrigin) {
  const parsed = new URL(runtimeOrigin);
  const suffixStart = parsed.hostname.indexOf(".");
  if (suffixStart < 0) { throw new Error("RPG_ACCEPTANCE_SECURITY_RUNTIME_HOST_INVALID"); }
  parsed.hostname = `not-a-launch${parsed.hostname.slice(suffixStart)}`;
  parsed.pathname = "/__retrom/entry";
  parsed.search = "";
  parsed.hash = "";
  return parsed.toString();
}
