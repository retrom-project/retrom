import type { LaunchEnvelopeV1, RuntimeWebResourceV1 } from "./contract";
export async function prepareIsolatedFrame(
  frame: HTMLIFrameElement,
  envelope: LaunchEnvelopeV1,
  resource: RuntimeWebResourceV1,
  signal: AbortSignal,
) {
  const runId = envelope.session.id;
  const url = new URL(
    `/__retrom/runtime-isolation/${runId}/shell.html`,
    resource.origin,
  );
  const bridgeResponse = await fetch(resource.bridgeUrl, {
    credentials: "same-origin",
    signal,
    redirect: "error",
  });
  if (!bridgeResponse.ok) {
    throw new Error("PLAYER_RUNTIME_BRIDGE_UNAVAILABLE");
  }
  const bridge = await bridgeResponse.text();
  if (bridge.length > 2 * 1024 * 1024) {
    throw new Error("PLAYER_RUNTIME_BRIDGE_INVALID");
  }
  const configuration = {
    parentOrigin: location.origin,
    runId,
    entryFile: resource.entryFile,
    bridge,
  };
  await new Promise<void>((resolve, reject) => {
    const timeout = setTimeout(
      () => finish(new Error("PLAYER_RUNTIME_FRAME_TIMEOUT")),
      10_000,
    );
    function finish(error?: Error) {
      clearTimeout(timeout);
      window.removeEventListener("message", receive);
      signal.removeEventListener("abort", aborted);
      if (error) {
        reject(error);
      } else {
        resolve();
      }
    }
    function aborted() {
      finish(new DOMException("Aborted", "AbortError"));
    }
    function receive(event: MessageEvent<unknown>) {
      if (
        event.source !== frame.contentWindow ||
        event.origin !== resource.origin ||
        !event.data ||
        typeof event.data !== "object" ||
        !("type" in event.data) ||
        event.data.type !== "RETROM_ISOLATION_READY"
      ) {
        return;
      }
      frame.contentWindow?.postMessage(
        { type: "RETROM_ISOLATION_CONFIGURE", configuration },
        resource.origin,
      );
      finish();
    }
    window.addEventListener("message", receive);
    signal.addEventListener("abort", aborted, { once: true });
    frame.src = url.href;
  });
}
