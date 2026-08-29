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
    if (!signal.aborted) {await task();}
  });
  lifecycle.tail = scheduled.catch(() => undefined);
  return scheduled;
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
  params: Params,
  createResources: () => Resources,
  bootstrap: (params: Params, resources: Resources, controller: AbortController) => Promise<void>,
  cleanup: (params: Params, resources: Resources, controller: AbortController) => Promise<void>,
  handleError: (error: unknown, controller: AbortController, params: Params) => void,
): void {
  const [lifecycle] = useState(createPlayerBootstrapLifecycle);
  useEffect(() => {
    const controller = new AbortController();
    const resources = createResources();
    const scheduled = schedulePlayerBootstrap(
      lifecycle, controller.signal, () => bootstrap(params, resources, controller),
    );
    void scheduled.catch((error: unknown) => handleError(error, controller, params));
    return () => {
      joinPlayerBootstrapCleanup(lifecycle, cleanup(params, resources, controller));
    };
  }, [bootstrap, cleanup, createResources, handleError, lifecycle, params]);
}
import { useEffect, useState } from "react";
