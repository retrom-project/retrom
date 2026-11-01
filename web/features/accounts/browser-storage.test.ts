import { afterEach, expect, it, vi } from "vitest";
import { clearManagedContentCache, readSiteUsage, type ContentCacheManager } from "./browser-storage";

afterEach(() => { vi.unstubAllGlobals(); });

it("reads browser usage without treating quota as device capacity", async () => {
  vi.stubGlobal("navigator", { storage: { estimate: async () => ({ usage: 1234, quota: Number.MAX_SAFE_INTEGER }) } });
  await expect(readSiteUsage()).resolves.toBe(1234);
});

it.each([undefined, -1, NaN, Infinity])("does not turn an unavailable estimate into zero: %s", async usage => {
  vi.stubGlobal("navigator", { storage: { estimate: async () => ({ usage }) } });
  await expect(readSiteUsage()).rejects.toThrow();
});

it("accepts a genuine empty-origin estimate", async () => {
  vi.stubGlobal("navigator", { storage: { estimate: async () => ({ usage: 0 }) } });
  await expect(readSiteUsage()).resolves.toBe(0);
});

it("continues beyond an entirely occupied batch and reports only actual removals", async () => {
  const clearContentCache = vi.fn()
    .mockResolvedValueOnce({ reclaimedBytes: 0, removedObjects: 0, skippedObjects: 64, nextKey: "a".repeat(64) })
    .mockResolvedValueOnce({ reclaimedBytes: 2000, removedObjects: 2, skippedObjects: 0, nextKey: null });
  const manager = { clearContentCache } as unknown as ContentCacheManager, signal = new AbortController().signal;
  const progress = vi.fn();
  await expect(clearManagedContentCache(manager, signal, progress)).resolves.toEqual({ reclaimedBytes: 2000, removedObjects: 2, skippedObjects: 64 });
  expect(clearContentCache).toHaveBeenLastCalledWith({ afterKey: "a".repeat(64), signal });
  expect(progress).toHaveBeenCalledTimes(2);
});

it("stops before admitting another batch after cancellation", async () => {
  const controller = new AbortController();
  const clearContentCache = vi.fn().mockResolvedValue({ reclaimedBytes: 2000, removedObjects: 1, skippedObjects: 0, nextKey: "a".repeat(64) });
  await expect(clearManagedContentCache({ clearContentCache } as unknown as ContentCacheManager, controller.signal,
    () => controller.abort())).rejects.toThrow();
  expect(clearContentCache).toHaveBeenCalledOnce();
});

it("rejects a nonadvancing cursor instead of looping over busy objects forever", async () => {
  const clearContentCache = vi.fn().mockResolvedValue({ reclaimedBytes: 0, removedObjects: 0, skippedObjects: 64, nextKey: "a".repeat(64) });
  await expect(clearManagedContentCache({ clearContentCache } as unknown as ContentCacheManager, new AbortController().signal, () => {})).rejects.toThrow("无法确认缓存清理结果");
  expect(clearContentCache).toHaveBeenCalledTimes(2);
});
