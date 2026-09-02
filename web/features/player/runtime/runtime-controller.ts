import type {LaunchEnvelopeV1, PlayerRuntimeV1} from "./contract";
import {loadProviderRuntime, type ProviderImporter} from "./provider-dispatcher";
import {createRuntimeHost, type RuntimeHostOptions} from "./runtime-host";

export type RuntimeController = {
  exit(): Promise<void>;
  runtime: PlayerRuntimeV1;
  signal: AbortSignal;
};

type ControllerOptions = {
  host?: RuntimeHostOptions;
  importer?: ProviderImporter;
  onExitRequested?: () => void;
  onFatalError?: (code: string) => void;
  signal?: AbortSignal;
};

export async function mountProviderRuntime(
  envelope: LaunchEnvelopeV1,
  target: HTMLElement,
  options: ControllerOptions = {},
): Promise<RuntimeController> {
  const abort = new AbortController();
  const host = createRuntimeHost(envelope, abort.signal, options.host);
  let runtime: PlayerRuntimeV1 | null = null;
  let unsubscribe: (() => void) | null = null;
  let exitPromise: Promise<void> | null = null;
  const exit = () => {
    exitPromise ??= (async () => {
      options.signal?.removeEventListener("abort", externalAbort);
      unsubscribe?.();
      unsubscribe = null;
      abort.abort();
      if (runtime) {await runtime.exit();}
    })();
    return exitPromise;
  };
  const externalAbort = () => {void exit();};
  if (options.signal?.aborted) {abort.abort(); throw new DOMException("Aborted", "AbortError");}
  options.signal?.addEventListener("abort", externalAbort, {once: true});
  try {
    runtime = await loadProviderRuntime(envelope, host, options.importer);
    unsubscribe = runtime.subscribe((event) => {
      if (event.type === "EXIT_REQUESTED") {options.onExitRequested?.();}
      if (event.type === "FATAL_ERROR") {options.onFatalError?.(event.code);}
    });
    await runtime.mount(target);
    return {exit, runtime, signal: abort.signal};
  } catch (error) {
    await exit().catch(() => undefined);
    throw error;
  } finally {
    if (exitPromise) {options.signal?.removeEventListener("abort", externalAbort);}
  }
}
