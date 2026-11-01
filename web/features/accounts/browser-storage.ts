import { api, result } from "@/lib/api/client";
import type { ContentCacheClearResultV1, ProviderModuleV1 } from "@/features/player/runtime/contract";
import { loadInstalledProviderModule } from "@/features/player/runtime/provider-dispatcher";

export type ContentCacheManager = Pick<ProviderModuleV1, "inspectContentCache" | "clearContentCache">;
export type ClearProgress = Omit<ContentCacheClearResultV1, "nextKey">;

export async function readSiteUsage(): Promise<number> {
  if (typeof navigator.storage?.estimate !== "function") {
    throw new Error("当前浏览器未提供站点占用估算。");
  }
  const { usage } = await navigator.storage.estimate();
  if (!Number.isSafeInteger(usage) || usage! < 0) {
    throw new Error("浏览器暂时无法估算站点占用。");
  }
  return usage!;
}

export async function loadContentCacheManager(signal: AbortSignal): Promise<ContentCacheManager> {
  const deadline = AbortSignal.any([signal, AbortSignal.timeout(15000)]);
  const modules = result(await api.GET("/api/v1/runtime/providers", { signal: deadline }));
  const reference = modules.items[0];
  if (!reference) { throw new Error("暂时没有可用的资源缓存管理模块。"); }
  // Providers share one content namespace; load one verified module and count it once.
  return loadInstalledProviderModule(reference, deadline);
}

export async function clearManagedContentCache(manager: ContentCacheManager, signal: AbortSignal,
  onProgress: (progress: ClearProgress) => void): Promise<ClearProgress> {
  const progress: ClearProgress = { reclaimedBytes: 0, removedObjects: 0, skippedObjects: 0 };
  let afterKey: string | undefined;
  do {
    signal.throwIfAborted();
    const batch = await manager.clearContentCache({ afterKey, signal });
    validateBatch(batch, afterKey);
    for (const key of ["reclaimedBytes", "removedObjects", "skippedObjects"] as const) {
      progress[key] += batch[key];
      if (!Number.isSafeInteger(progress[key])) { throw new Error("缓存清理结果超出可统计范围，请刷新后重试。"); }
    }
    onProgress({ ...progress });
    afterKey = batch.nextKey ?? undefined;
  } while (afterKey !== undefined);
  return progress;
}

function validateBatch(batch: ContentCacheClearResultV1, afterKey?: string) {
  if ([batch.reclaimedBytes, batch.removedObjects, batch.skippedObjects].some(value => !Number.isSafeInteger(value) || value < 0) ||
    batch.nextKey !== null && (!/^[a-f0-9]{64}$/u.test(batch.nextKey) || afterKey !== undefined && batch.nextKey <= afterKey)) {
    throw new Error("无法确认缓存清理结果，请刷新后重试。");
  }
}
