import { useEffect, useRef, useState } from "react";

export type PlayerBootstrapLifecycle = {
  tail: Promise<void>;
};

export function createPlayerBootstrapLifecycle(): PlayerBootstrapLifecycle {
  return { tail: Promise.resolve() };
}

export function schedulePlayerBootstrap(
  lifecycle: PlayerBootstrapLifecycle,
  signal: AbortSignal,
  task: () => Promise<void>,
): Promise<void> {
  const scheduled = lifecycle.tail.then(async () => {
    await waitForBootstrapTurn(signal);
    if (!signal.aborted) {await task();}
  });
  lifecycle.tail = scheduled.catch(() => undefined);
  return scheduled;
}

function waitForBootstrapTurn(signal: AbortSignal): Promise<void> {
  if (signal.aborted) {return Promise.resolve();}
  return new Promise((resolve) => {
    const timer = window.setTimeout(finish, 0);
    signal.addEventListener("abort", finish, { once: true });
    function finish() {
      window.clearTimeout(timer);
      signal.removeEventListener("abort", finish);
      resolve();
    }
  });
}

export function joinPlayerBootstrapCleanup(
  lifecycle: PlayerBootstrapLifecycle,
  cleanup: Promise<void>,
): void {
  lifecycle.tail = Promise.all([
    lifecycle.tail,
    cleanup.catch(() => undefined),
  ]).then(() => undefined);
}

export function useSerializedPlayerBootstrap<Params, Resources>(
  bootstrapKey: string,
  params: Params,
  createResources: () => Resources,
  bootstrap: (params: Params, resources: Resources, controller: AbortController) => Promise<void>,
  cleanup: (params: Params, resources: Resources, controller: AbortController) => Promise<void>,
  handleError: (error: unknown, controller: AbortController, params: Params) => void,
): void {
  const [lifecycle] = useState(createPlayerBootstrapLifecycle);
  const latestParams = useRef(params);
  useEffect(() => {latestParams.current = params;}, [params]);
  useEffect(() => {
    const activeParams = latestParams.current;
    const controller = new AbortController();
    const resources = createResources();
    const scheduled = schedulePlayerBootstrap(
      lifecycle, controller.signal, () => bootstrap(activeParams, resources, controller),
    );
    void scheduled.catch((error: unknown) => handleError(error, controller, activeParams));
    return () => {
      joinPlayerBootstrapCleanup(lifecycle, cleanup(activeParams, resources, controller));
    };
  }, [bootstrap, bootstrapKey, cleanup, createResources, handleError, lifecycle]);
}
