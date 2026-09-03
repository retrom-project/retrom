import {afterEach, describe, expect, test, vi} from "vitest";
import type {PlayerRuntimeV1} from "./contract";
import {installRuntimeE2EDiagnostics} from "./e2e-diagnostics";

describe("runtime E2E diagnostics", () => {
  afterEach(() => {delete window.__RETROM_E2E_RUNTIME_V1__;});

  test("only projects observations from the standard PlayerRuntimeV1 contract", async () => {
    const runtime = {
      getState: vi.fn(() => "RUNNING"),
      getFrameCount: vi.fn(() => 42),
      checkpoint: vi.fn(async () => ({
        format: "opaque-v1",
        bytes: Uint8Array.from([1, 2, 3]),
        metadata: {private: "not exposed"},
      })),
    } as unknown as PlayerRuntimeV1;

    const cleanup = installRuntimeE2EDiagnostics(runtime, "development");
    expect(window.__RETROM_E2E_RUNTIME_V1__?.getState()).toBe("RUNNING");
    expect(window.__RETROM_E2E_RUNTIME_V1__?.getFrameCount()).toBe(42);
    await expect(window.__RETROM_E2E_RUNTIME_V1__?.checkpoint()).resolves.toEqual({
      format: "opaque-v1",
      sha256: "039058c6f2c0cb492c533b0a4d14ef77cc0f78abccced5287d84a1a2011cfb81",
      sizeBytes: 3,
    });
    expect(runtime.checkpoint).toHaveBeenCalledOnce();

    cleanup();
    expect(window.__RETROM_E2E_RUNTIME_V1__).toBeUndefined();
  });

  test("is absent from production and cleanup cannot remove a newer runtime", () => {
    const first = {} as PlayerRuntimeV1;
    const second = {} as PlayerRuntimeV1;
    installRuntimeE2EDiagnostics(first, "production");
    expect(window.__RETROM_E2E_RUNTIME_V1__).toBeUndefined();

    const cleanupFirst = installRuntimeE2EDiagnostics(first, "development");
    installRuntimeE2EDiagnostics(second, "development");
    cleanupFirst();
    expect(window.__RETROM_E2E_RUNTIME_V1__).toBeDefined();
  });
});
