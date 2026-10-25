import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it, vi } from "vitest";
import type { LaunchEnvelopeV1 } from "./contract";
import * as dispatcher from "./provider-dispatcher";

function fixture() {
  const envelope = JSON.parse(readFileSync(resolve(process.cwd(), "../api/runtime-provider/v1/fixtures/valid/single-minimal.json"), "utf8")) as LaunchEnvelopeV1;
  const provider = {
    providerId: envelope.runtime.providerId,
    providerVersion: envelope.runtime.providerVersion,
    providerApiVersion: 1,
    createRuntime: vi.fn(),
    reclaimContentCache: vi.fn().mockResolvedValue({ reclaimedBytes: 123456, removedObjects: 1 }),
  };
  const importer = vi.fn().mockResolvedValue(provider);
  const environment = {
    createModuleUrl: vi.fn().mockReturnValue("blob:verified-provider"),
    revokeModuleUrl: vi.fn(),
    crossOriginIsolated: false,
    sha256: vi.fn().mockResolvedValue(envelope.runtime.moduleSha256),
    fetcher: vi.fn().mockImplementation(async () => new Response("verified module bytes", { headers: { "content-type": "text/javascript; charset=utf-8" } })),
  };
  return { envelope, provider, importer, environment };
}

describe("verified Provider content-cache recovery", () => {
  it("recovers without a PlayerRuntime, iframe, threads or Wasm after a final snapshot", async () => {
    const f = fixture();
    f.envelope.runtime.capabilities.requiresThreads = true;
    const request = { targetBytes: 32 * 1024 * 1024 };
    expect(typeof dispatcher.reclaimProviderContentCache).toBe("function");
    await expect(dispatcher.reclaimProviderContentCache(f.envelope, request, f.importer, f.environment)).resolves.toEqual({ reclaimedBytes: 123456, removedObjects: 1 });
    expect(f.provider.createRuntime).not.toHaveBeenCalled();
    expect(f.provider.reclaimContentCache).toHaveBeenCalledWith(expect.objectContaining(request));
    const signal = f.provider.reclaimContentCache.mock.calls[0][0].signal;
    expect(signal).toBeInstanceOf(AbortSignal);
    expect(f.environment.fetcher).toHaveBeenCalledWith(f.envelope.runtime.moduleUrl, expect.objectContaining({ credentials: "same-origin", redirect: "error", signal }));
    expect(f.environment.revokeModuleUrl).toHaveBeenCalledWith("blob:verified-provider");
  });

  it("never executes cache deletion from bytes with an invalid digest", async () => {
    const f = fixture();
    f.environment.sha256.mockResolvedValue("f".repeat(64));
    await expect(dispatcher.reclaimProviderContentCache(f.envelope, { targetBytes: 1 }, f.importer, f.environment)).rejects.toThrow("PLAYER_PROVIDER_MODULE_DIGEST_INVALID");
    expect(f.importer).not.toHaveBeenCalled();
    expect(f.provider.reclaimContentCache).not.toHaveBeenCalled();
  });

  it.each(["identity", "method", "extra-export"])("rejects an invalid closed module before recovery: %s", async (invalid) => {
    const f = fixture();
    if (invalid === "identity") { f.provider.providerId = "other-provider"; }
    if (invalid === "method") { Reflect.deleteProperty(f.provider, "reclaimContentCache"); }
    if (invalid === "extra-export") { Reflect.set(f.provider, "clearOrigin", vi.fn()); }
    await expect(dispatcher.reclaimProviderContentCache(f.envelope, { targetBytes: 1 }, f.importer, f.environment)).rejects.toThrow("PLAYER_PROVIDER_MODULE_INVALID");
    expect(f.provider.createRuntime).not.toHaveBeenCalled();
    if (invalid !== "method") { expect(f.provider.reclaimContentCache).not.toHaveBeenCalled(); }
  });

  it("preserves a cache recovery failure for the save's original cause chain", async () => {
    const f = fixture();
    const failure = new DOMException("Cache deletion denied", "SecurityError");
    f.provider.reclaimContentCache.mockRejectedValue(failure);
    await expect(dispatcher.reclaimProviderContentCache(f.envelope, { targetBytes: 1 }, f.importer, f.environment)).rejects.toBe(failure);
  });

  it("honors cancellation before touching module or cache storage", async () => {
    const f = fixture();
    const controller = new AbortController();
    const failure = new DOMException("Owner cancelled", "AbortError");
    controller.abort(failure);
    await expect(dispatcher.reclaimProviderContentCache(f.envelope, { targetBytes: 1, signal: controller.signal }, f.importer, f.environment)).rejects.toBe(failure);
    expect(f.environment.fetcher).not.toHaveBeenCalled();
    expect(f.provider.reclaimContentCache).not.toHaveBeenCalled();
  });
});
