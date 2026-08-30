import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  createPlayerBootstrapLifecycle,
  joinPlayerBootstrapCleanup,
  schedulePlayerBootstrap,
  useSerializedPlayerBootstrap,
} from "./player-bootstrap-lifecycle";

describe("player bootstrap lifecycle", () => {
  it("waits for an aborted mount and its asynchronous cleanup before remounting", async () => {
    const lifecycle = createPlayerBootstrapLifecycle();
    const first = new AbortController();
    let finishMount: (() => void) | undefined;
    let finishCleanup: (() => void) | undefined;
    const actions: string[] = [];
    const mounted = schedulePlayerBootstrap(lifecycle, first.signal, async () => {
      actions.push("mount:first");
      await new Promise<void>((resolve) => {finishMount = resolve;});
      actions.push("mount:first:finished");
    });
    await new Promise((resolve) => window.setTimeout(resolve, 10));
    first.abort();
    joinPlayerBootstrapCleanup(lifecycle, new Promise<void>((resolve) => {
      actions.push("cleanup:first");
      finishCleanup = resolve;
    }));
    const second = new AbortController();
    const remounted = schedulePlayerBootstrap(lifecycle, second.signal, async () => {
      actions.push("mount:second");
    });

    await Promise.resolve();
    expect(actions).toEqual(["mount:first", "cleanup:first"]);
    finishMount?.();
    await mounted;
    await Promise.resolve();
    expect(actions).toEqual(["mount:first", "cleanup:first", "mount:first:finished"]);
    finishCleanup?.();
    await remounted;
    expect(actions).toEqual([
      "mount:first", "cleanup:first", "mount:first:finished", "mount:second",
    ]);
  });

  it("does not mount the Strict Mode probe that aborts in the same turn", async () => {
    const lifecycle = createPlayerBootstrapLifecycle();
    const probe = new AbortController();
    const actions: string[] = [];
    const probed = schedulePlayerBootstrap(lifecycle, probe.signal, async () => {
      actions.push("probe");
    });
    probe.abort();
    joinPlayerBootstrapCleanup(lifecycle, Promise.resolve());
    const actual = schedulePlayerBootstrap(lifecycle, new AbortController().signal, async () => {
      actions.push("actual");
    });

    await Promise.all([probed, actual]);
    expect(actions).toEqual(["actual"]);
  });

  it("does not let a failed bootstrap block the next mount", async () => {
    const lifecycle = createPlayerBootstrapLifecycle();
    const failed = schedulePlayerBootstrap(lifecycle, new AbortController().signal, async () => {
      throw new Error("mount failed");
    });
    await expect(failed).rejects.toThrow("mount failed");
    const actions: string[] = [];
    await schedulePlayerBootstrap(lifecycle, new AbortController().signal, async () => {
      actions.push("mounted");
    });
    expect(actions).toEqual(["mounted"]);
  });

  it("does not restart an active launch when render callbacks change", async () => {
    const bootstrap = vi.fn(async () => undefined);
    const cleanup = vi.fn(async () => undefined);
    const createResources = vi.fn(() => ({}));
    const handleError = vi.fn();
    const { rerender, unmount } = renderHook(
      ({ key, params }: { key: string; params: { revision: number } }) =>
        useSerializedPlayerBootstrap(
          key, params, createResources, bootstrap, cleanup, handleError,
        ),
      { initialProps: { key: "launch-a:standard", params: { revision: 1 } } },
    );
    await waitFor(() => expect(bootstrap).toHaveBeenCalledTimes(1));

    rerender({ key: "launch-a:standard", params: { revision: 2 } });
    await new Promise((resolve) => window.setTimeout(resolve, 10));
    expect(bootstrap).toHaveBeenCalledTimes(1);
    expect(cleanup).not.toHaveBeenCalled();

    rerender({ key: "launch-b:standard", params: { revision: 3 } });
    await waitFor(() => expect(bootstrap).toHaveBeenCalledTimes(2));
    expect(cleanup).toHaveBeenCalledTimes(1);
    unmount();
  });
});
