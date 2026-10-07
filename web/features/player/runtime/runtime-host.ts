import type {
  LaunchEnvelopeV1,
  RuntimeHostV1,
  RuntimeWebResourceV1,
} from "./contract";
import { prepareIsolatedFrame } from "./isolated-frame";
import { prepareBlankFrame } from "./blank-frame";
import type { ContentLoading } from "../content-loading";
export function createRuntimeHost(
  envelope: LaunchEnvelopeV1,
  signal: AbortSignal,
  localRestore: Uint8Array | null = null,
  contentLoading: ContentLoading = "ON_DEMAND",
): RuntimeHostV1 {
  const frames = new Set<HTMLIFrameElement>();
  signal.addEventListener(
    "abort",
    () => {
      for (const frame of frames) {
        frame.remove();
      }
      frames.clear();
    },
    { once: true },
  );
  return {
    signal,
    contentLoading:
      envelope.runtime.capabilities.contentLoading === "PRELOAD_ONLY"
        ? "PRELOAD"
        : contentLoading,
    async mountFrame(target, input) {
      if (signal.aborted) {
        throw new DOMException("Aborted", "AbortError");
      }
      const frame = document.createElement("iframe");
      frame.className = "player-frame";
      frame.allow = "autoplay; fullscreen; gamepad";
      frame.referrerPolicy = "no-referrer";
      frame.setAttribute(
        "sandbox",
        "allow-scripts allow-same-origin allow-pointer-lock",
      );
      frames.add(frame);
      if (
        input.resourceRole === null &&
        envelope.runtime.capabilities.frameMode === "SAME_ORIGIN_BLANK"
      ) {
        await prepareBlankFrame(frame, target, signal);
      } else {
        target.append(frame);
        const resource = envelope.resources.find(
          (item) => item.role === input.resourceRole && item.ordinal === 0,
        );
        if (
          !resource ||
          !isWebResource(resource) ||
          resource.origin === location.origin
        ) {
          throw new Error("PLAYER_RUNTIME_FRAME_INVALID");
        }
        await prepareIsolatedFrame(frame, envelope, resource, signal);
      }
      if (!frame.contentWindow) {
        throw new Error("PLAYER_RUNTIME_FRAME_INVALID");
      }
      return {
        element: frame,
        contentWindow: frame.contentWindow,
        origin:
          input.resourceRole === null
            ? location.origin
            : new URL(frame.src).origin,
      };
    },
    async loadRestore(descriptor) {
      if (!descriptor) {
        return null;
      }
      if (descriptor.kind === "LOCAL") {
        if (
          !localRestore ||
          localRestore.length !== descriptor.sizeBytes ||
          (await digest(localRestore)) !== descriptor.sha256
        ) {
          throw new Error("PLAYER_RUNTIME_RESTORE_INVALID");
        }
        return Uint8Array.from(localRestore);
      }
      if (
        !descriptor.url.startsWith("/api/v1/saves/") ||
        !envelope.runtime.checkpoint?.readFormats.includes(descriptor.format) ||
        descriptor.sizeBytes > envelope.runtime.checkpoint.maxBytes
      ) {
        throw new Error("PLAYER_RUNTIME_RESTORE_INVALID");
      }
      const response = await fetch(descriptor.url, {
        credentials: "same-origin",
        signal,
        cache: "no-store",
        redirect: "error",
      });
      if (!response.ok) {
        throw new Error("PLAYER_RUNTIME_RESTORE_UNAVAILABLE");
      }
      const bytes = new Uint8Array(await response.arrayBuffer());
      if (
        bytes.length !== descriptor.sizeBytes ||
        (await digest(bytes)) !== descriptor.sha256
      ) {
        throw new Error("PLAYER_RUNTIME_RESTORE_INVALID");
      }
      return bytes;
    },
    reportDiagnostic(input) {
      window.dispatchEvent(
        new CustomEvent("retrom:runtime-diagnostic", { detail: input }),
      );
    },
  };
}
function isWebResource(
  value: LaunchEnvelopeV1["resources"][number],
): value is RuntimeWebResourceV1 {
  return value.kind === "NATIVE_WEB" || value.kind === "ISOLATED_WEB";
}
async function digest(bytes: Uint8Array) {
  const value = await crypto.subtle.digest(
    "SHA-256",
    Uint8Array.from(bytes).buffer,
  );
  return [...new Uint8Array(value)]
    .map((byte) => byte.toString(16).padStart(2, "0"))
    .join("");
}
