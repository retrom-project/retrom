import { describe, expect, it } from "vitest";
import {
  createPlayerBootstrapLifecycle,
  joinPlayerBootstrapCleanup,
  schedulePlayerBootstrap,
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
    await Promise.resolve();
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
});
