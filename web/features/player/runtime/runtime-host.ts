import type {
  LaunchEnvelopeV1,
  RestoreDescriptorV1,
  RuntimeFrameV1,
  RuntimeHostV1,
  RuntimeResourceV1,
} from "./contract";
import {playerRuntimeError} from "./errors";

export type RuntimeHostOptions = {
  fetcher?: typeof fetch;
  report?: (input: {code: string; message: string}) => void;
  sha256?: (bytes: Uint8Array) => Promise<string>;
};

const sandboxTokens = ["allow-downloads", "allow-pointer-lock", "allow-same-origin", "allow-scripts"];

export function createRuntimeHost(
  envelope: LaunchEnvelopeV1,
  signal: AbortSignal,
  options: RuntimeHostOptions = {},
): RuntimeHostV1 {
  const fetcher = options.fetcher ?? fetch;
  const frames = new Set<HTMLIFrameElement>();
  const cleanups = new Set<string>();
  let cleanupPromise: Promise<void> | null = null;
  const cleanup = () => {
    if (cleanupPromise) {return cleanupPromise;}
    for (const frame of frames) {frame.remove();}
    frames.clear();
    const cleanupUrls = [...cleanups];
    cleanups.clear();
    cleanupPromise = Promise.all([
      ...cleanupUrls.map((url) => fetcher(url, {
        credentials: "include", keepalive: true, method: "POST",
      }).then(() => undefined).catch(() => undefined)),
    ]).then(() => undefined);
    return cleanupPromise;
  };
  signal.addEventListener("abort", () => {void cleanup();}, {once: true});

  return {
    signal,
    async mountFrame(target, input) {
      if (signal.aborted || !target.isConnected && target.ownerDocument !== document) {frameError();}
      const source = frameSource(envelope, input.resourceRole);
      const frame = document.createElement("iframe");
      frame.referrerPolicy = "no-referrer";
      frame.allow = "autoplay; fullscreen; gamepad";
      frame.setAttribute("sandbox", sandboxTokens.join(" "));
      frame.src = source.url;
      target.append(frame);
      const contentWindow = frame.contentWindow;
      if (!contentWindow) {frame.remove(); frameError();}
      frames.add(frame);
      if (source.cleanupUrl) {cleanups.add(source.cleanupUrl);}
      return {contentWindow, element: frame, origin: source.origin} satisfies RuntimeFrameV1;
    },
    async loadRestore(descriptor) {
      if (descriptor === null) {return null;}
      validateRestore(envelope, descriptor);
      let response: Response;
      try {
        response = await fetcher(descriptor.url, {
          cache: "no-store", credentials: "same-origin", signal,
        });
      } catch {restoreError();}
      if (!response.ok) {restoreError();}
      const contentLength = response.headers.get("content-length");
      if (contentLength !== null && Number(contentLength) !== descriptor.sizeBytes) {restoreError();}
      const bytes = new Uint8Array(await response.arrayBuffer());
      if (bytes.byteLength !== descriptor.sizeBytes) {restoreError();}
      const actual = await (options.sha256 ?? digestSha256)(bytes);
      if (actual !== descriptor.sha256) {restoreError();}
      return bytes;
    },
    reportDiagnostic(input) {
      if (!diagnostic(input)) {throw playerRuntimeError("PLAYER_RUNTIME_DIAGNOSTIC_INVALID");}
      if (options.report) {options.report(input); return;}
      window.dispatchEvent(new CustomEvent("retrom:runtime-diagnostic", {detail: input}));
    },
  };
}

function frameSource(envelope: LaunchEnvelopeV1, resourceRole: string | null) {
  const mode = envelope.runtime.capabilities.frameMode;
  if (mode === "SAME_ORIGIN_BLANK" && resourceRole === null) {
    return {cleanupUrl: null, origin: location.origin, url: "about:blank"};
  }
  if (mode === "NONE" || mode === "SAME_ORIGIN_BLANK" || resourceRole === null) {frameError();}
  const expectedKind = mode === "SAME_ORIGIN_RESOURCE" ? "NATIVE_WEB_V1" : "ISOLATED_WEB_V1";
  const resource = envelope.resources.find((entry) => entry.role === resourceRole && entry.ordinal === 0);
  if (!resource || resource.kind !== expectedKind || !validWebResource(resource)) {frameError();}
  if (mode === "SAME_ORIGIN_RESOURCE" && resource.origin !== location.origin) {frameError();}
  if (mode === "ISOLATED_ORIGIN_RESOURCE" && resource.origin === location.origin) {frameError();}
  return {cleanupUrl: resource.cleanupUrl, origin: resource.origin, url: resource.entryUrl};
}

function validWebResource(resource: RuntimeResourceV1): resource is Extract<RuntimeResourceV1, {
  kind: "NATIVE_WEB_V1" | "ISOLATED_WEB_V1";
}> {
  if (resource.kind !== "NATIVE_WEB_V1" && resource.kind !== "ISOLATED_WEB_V1") {return false;}
  try {
    const entry = new URL(resource.entryUrl, location.href);
    const origin = new URL(resource.origin);
    const cleanup = resource.cleanupUrl === null ? null : new URL(resource.cleanupUrl, location.href);
    return origin.href === `${origin.origin}/` && entry.origin === origin.origin &&
      (cleanup === null || cleanup.origin === origin.origin);
  } catch {return false;}
}

function validateRestore(envelope: LaunchEnvelopeV1, descriptor: RestoreDescriptorV1) {
  const checkpoint = envelope.runtime.checkpoint;
  if (!checkpoint || descriptor.format.length < 1 || descriptor.sizeBytes < 1 ||
    !checkpoint.readFormats.includes(descriptor.format) || descriptor.sizeBytes > checkpoint.maxBytes ||
    !/^[0-9a-f]{64}$/u.test(descriptor.sha256) ||
    !sameOriginRelativeUrl(descriptor.url)) {restoreError();}
}

function sameOriginRelativeUrl(value: string) {
  if (!value.startsWith("/") || value.startsWith("//") || value.includes("\\") || value.includes("#")) {
    return false;
  }
  try {return new URL(value, location.href).origin === location.origin;} catch {return false;}
}

async function digestSha256(bytes: Uint8Array) {
  const digest = await crypto.subtle.digest("SHA-256", bytes as Uint8Array<ArrayBuffer>);
  return [...new Uint8Array(digest)].map((value) => value.toString(16).padStart(2, "0")).join("");
}

function diagnostic(value: {code: string; message: string}) {
  return /^[A-Z][A-Z0-9_]{1,127}$/u.test(value.code) && value.message.length >= 1 && value.message.length <= 4096;
}

function frameError(): never {throw playerRuntimeError("PLAYER_RUNTIME_FRAME_INVALID");}
function restoreError(): never {throw playerRuntimeError("PLAYER_RUNTIME_RESTORE_INVALID");}
