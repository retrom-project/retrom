"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { TagReference } from "@/components/tag-picker";
import { api, writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import { responseError } from "@/lib/upload";
import type { ServerImportRoot } from "./server-import-manager";
import {
  pegasusStateLabels,
  pegasusStateTone,
  type PegasusCollection,
  type PegasusDirectory,
  type PegasusImportList,
  type PegasusImportSummary,
  type PegasusItemList,
  type PegasusItem,
  type PegasusPlatformInstance,
} from "./pegasus-import-model";
import {
  PegasusImportDetailView,
  PegasusImportDrawerView,
  type DetailFilters,
  type MappingDraft,
} from "./pegasus-import-view";

export {
  pegasusStateLabels,
  pegasusStateTone,
  type PegasusCollection,
  type PegasusImportList,
  type PegasusImportSummary,
  type PegasusItemList,
  type PegasusItem,
  type PegasusPlatformInstance,
};

function message(response: Response, fallback: string) {
  return responseError(response, fallback);
}

function mergeTags(current: TagReference[], additions: TagReference[]) {
  const merged = new Map(current.map((tag) => [tag.tagId, tag]));
  additions.forEach((tag) => merged.set(tag.tagId, tag));
  return [...merged.values()].sort((left, right) => left.name.localeCompare(right.name, "zh-CN"));
}

export function PegasusImportDrawer({ open, roots, platformInstances, activeTags = [], resumablePlan, onClose, onStarted }: {
  open: boolean;
  roots: ServerImportRoot[];
  platformInstances: PegasusPlatformInstance[];
  activeTags?: TagReference[];
  resumablePlan?: PegasusImportSummary;
  onClose: () => void;
  onStarted: (summary: PegasusImportSummary) => void;
}) {
  const router = useRouter();
  const hydratedPlanId = useRef("");
  const refreshRequest = useRef<{ planId: string; promise: Promise<PegasusImportSummary> } | null>(null);
  const [step, setStep] = useState<1 | 2 | 3>(resumablePlan ? 2 : 1);
  const [rootId, setRootId] = useState(resumablePlan?.root.id ?? roots.find((root) => root.status === "AVAILABLE")?.id ?? "");
  const [path, setPath] = useState(resumablePlan?.sourceRelativePath ?? "");
  const [directories, setDirectories] = useState<PegasusDirectory[]>([]);
  const [directoryCursor, setDirectoryCursor] = useState<string | null>(null);
  const [directoryLoading, setDirectoryLoading] = useState(false);
  const [plan, setPlan] = useState<PegasusImportSummary | null>(resumablePlan ?? null);
  const [collections, setCollections] = useState<PegasusCollection[]>([]);
  const [mappings, setMappings] = useState<Record<string, MappingDraft>>({});
  const [batchTags, setBatchTags] = useState<TagReference[]>([]);
  const [batchTagStatus, setBatchTagStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const availableInstances = useMemo(() => platformInstances.filter((instance) => instance.enabled), [platformInstances]);
  const breadcrumbs = useMemo(() => path ? path.split("/") : [], [path]);
  const selectedRoot = roots.find((root) => root.id === rootId);

  const loadCollections = useCallback(async (planId: string) => {
    const all: PegasusCollection[] = [];
    let cursor: string | undefined;
    do {
      const { data, response } = await api.GET("/api/v1/admin/pegasus-imports/{pegasusImportId}/collections", {
        params: { path: { pegasusImportId: planId }, query: { cursor, limit: 100 } },
      });
      if (!data) {throw new Error(await message(response, "Pegasus 集合读取失败"));}
      all.push(...data.items);
      cursor = data.nextCursor ?? undefined;
    } while (cursor);
    setCollections(all);
    setBatchTags([]);
    setBatchTagStatus("");
    setMappings(Object.fromEntries(all.map((collection) => [collection.id, {
      action: collection.mappingAction ?? "",
      platformInstanceId: collection.targetPlatformInstanceId ?? "",
      tags: collection.tagSnapshot ?? [],
    }])));
    return all;
  }, []);

  const refreshPlan = useCallback((planId: string) => {
    if (refreshRequest.current?.planId === planId) {return refreshRequest.current.promise;}
    const promise = (async () => {
      const { data, response } = await api.GET("/api/v1/admin/pegasus-imports/{pegasusImportId}", { params: { path: { pegasusImportId: planId } } });
      if (!data) {throw new Error(await message(response, "Pegasus 计划读取失败"));}
      setPlan(data);
      if (data.state === "AWAITING_MAPPING") {
        const loaded = await loadCollections(data.id);
        const complete = loaded.length > 0 && loaded.every((collection) => collection.mappingAction === "SKIP" || collection.mappingAction === "IMPORT" && Boolean(collection.targetPlatformInstanceId));
        setStep(complete ? 3 : 2);
      }
      return data;
    })();
    refreshRequest.current = { planId, promise };
    void promise.then(
      () => {if (refreshRequest.current?.promise === promise) {refreshRequest.current = null;}},
      () => {if (refreshRequest.current?.promise === promise) {refreshRequest.current = null;}},
    );
    return promise;
  }, [loadCollections]);

  const resumablePlanId = resumablePlan?.id ?? "";
  useEffect(() => {
    if (!open) {hydratedPlanId.current = ""; return;}
    if (!resumablePlanId || hydratedPlanId.current === resumablePlanId) {return;}
    let active = true;
    queueMicrotask(() => {
      if (!active) {return;}
      hydratedPlanId.current = resumablePlanId;
      void refreshPlan(resumablePlanId).catch((caught: unknown) => setError(caught instanceof Error ? caught.message : "Pegasus 计划读取失败"));
    });
    return () => {active = false;};
  }, [open, refreshPlan, resumablePlanId]);

  useEffect(() => {
    if (!open || step !== 1 || !rootId) {return;}
    const controller = new AbortController();
    queueMicrotask(() => {if (!controller.signal.aborted) {setDirectoryLoading(true);}});
    void api.GET("/api/v1/admin/server-import-roots/{rootId}/directories", {
      params: { path: { rootId }, query: { path, limit: 100 } }, signal: controller.signal,
    }).then(async ({ data, response }) => {
      if (!data) {throw new Error(await message(response, "服务器目录读取失败"));}
      setDirectories(data.items); setDirectoryCursor(data.nextCursor);
    }).catch((caught: unknown) => {
      if (!(caught instanceof DOMException && caught.name === "AbortError")) {setError(caught instanceof Error ? caught.message : "服务器目录读取失败");}
    }).finally(() => setDirectoryLoading(false));
    return () => controller.abort();
  }, [open, path, rootId, step]);

  useEffect(() => {
    if (!open || !plan || plan.state !== "SCANNING") {return;}
    const update = () => void refreshPlan(plan.id).catch((caught: unknown) => setError(caught instanceof Error ? caught.message : "扫描进度读取失败"));
    const timer = window.setInterval(update, 2_000);
    const source = typeof EventSource === "undefined" ? null : new EventSource(`/api/v1/admin/jobs/${encodeURIComponent(plan.scanJobId)}/events`, { withCredentials: true });
    for (const event of ["progress", "succeeded", "failed"]) {source?.addEventListener(event, update);}
    return () => {window.clearInterval(timer); source?.close();};
  }, [open, plan, refreshPlan]);

  async function loadMoreDirectories() {
    if (!directoryCursor || directoryLoading) {return;}
    setDirectoryLoading(true); setError("");
    try {
      const { data, response } = await api.GET("/api/v1/admin/server-import-roots/{rootId}/directories", { params: { path: { rootId }, query: { path, cursor: directoryCursor, limit: 100 } } });
      if (!data) {throw new Error(await message(response, "服务器目录读取失败"));}
      setDirectories((current) => [...current, ...data.items.filter((item) => !current.some((known) => known.relativePath === item.relativePath))]);
      setDirectoryCursor(data.nextCursor);
    } catch (caught) {setError(caught instanceof Error ? caught.message : "服务器目录读取失败");}
    finally {setDirectoryLoading(false);}
  }

  async function scan() {
    if (!rootId || selectedRoot?.status !== "AVAILABLE") {return;}
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/pegasus-imports", { params: { header: { ...writeHeaders(), "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } }, body: { rootId, sourceRelativePath: path } });
      if (!data) {throw new Error(await message(response, "Pegasus 扫描创建失败"));}
      hydratedPlanId.current = data.id;
      setPlan(data); setStep(2); onStarted(data);
    } catch (caught) {setError(caught instanceof Error ? caught.message : "Pegasus 扫描创建失败");}
    finally {setBusy(false);}
  }

  async function confirmMappings() {
    if (!plan || collections.some((collection) => !mappings[collection.id]?.action) || collections.some((collection) => mappings[collection.id]?.action === "IMPORT" && !mappings[collection.id]?.platformInstanceId)) {return;}
    setBusy(true); setError("");
    try {
      let current = plan;
      const values = collections.map((collection) => {
        const draft = mappings[collection.id];
        return draft.action === "SKIP" ? { collectionId: collection.id, action: "SKIP" as const, tagIds: [] } : { collectionId: collection.id, action: "IMPORT" as const, platformInstanceId: draft.platformInstanceId, tagIds: draft.tags.map((tag) => tag.tagId) };
      });
      for (let offset = 0; offset < values.length; offset += 100) {
        const { data, response } = await api.PUT("/api/v1/admin/pegasus-imports/{pegasusImportId}/collection-mappings", { params: { path: { pegasusImportId: current.id }, header: { ...writeHeaders(), "If-Match": `"v${current.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } }, body: { mappings: values.slice(offset, offset + 100) } });
        if (!data) {throw new Error(await message(response, "集合映射保存失败"));}
        current = data;
      }
      setPlan(current); setStep(3);
    } catch (caught) {setError(caught instanceof Error ? caught.message : "集合映射保存失败");}
    finally {setBusy(false);}
  }

  function applyBatchTags() {
    if (!batchTags.length) {return;}
    const targets = collections.filter((collection) => mappings[collection.id]?.action !== "SKIP");
    const oversized = targets.find((collection) => mergeTags(mappings[collection.id]?.tags ?? [], batchTags).length > 20);
    if (oversized) {setError(`无法批量添加：${oversized.name} 合并后会超过 20 个标签。请先移除部分标签。`); return;}
    setMappings((current) => {
      const next = { ...current };
      targets.forEach((collection) => {
        const draft = current[collection.id] ?? { action: "", platformInstanceId: "", tags: [] };
        next[collection.id] = { ...draft, tags: mergeTags(draft.tags, batchTags) };
      });
      return next;
    });
    const gameCount = targets.reduce((total, collection) => total + collection.gameCount, 0);
    setBatchTagStatus(`已追加到 ${targets.length} 个未跳过 Collection，覆盖 ${gameCount} 个游戏；仍可在下方逐项调整。`);
  }

  async function startImport() {
    if (!plan) {return;}
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/pegasus-imports/{pegasusImportId}/start", { params: { path: { pegasusImportId: plan.id }, header: { ...writeHeaders(), "If-Match": `"v${plan.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } }, body: { version: plan.version } });
      if (!data) {throw new Error(await message(response, "Pegasus 导入启动失败"));}
      onStarted(data); onClose(); router.push(`/admin/imports/server/pegasus/${data.id}`);
    } catch (caught) {setError(caught instanceof Error ? caught.message : "Pegasus 导入启动失败");}
    finally {setBusy(false);}
  }

  const mapped = Object.values(mappings).filter((mapping) => mapping.action === "IMPORT").length;
  const skipped = Object.values(mappings).filter((mapping) => mapping.action === "SKIP").length;
  const taggedCollections = collections.filter((collection) => mappings[collection.id]?.action === "IMPORT" && mappings[collection.id]?.tags.length);
  const taggedGames = taggedCollections.reduce((total, collection) => total + collection.gameCount, 0);
  const mappedTags = mergeTags([], taggedCollections.flatMap((collection) => mappings[collection.id]?.tags ?? []));
  const mappingComplete = collections.length > 0 && mapped + skipped === collections.length && collections.every((collection) => mappings[collection.id]?.action !== "IMPORT" || Boolean(mappings[collection.id]?.platformInstanceId));
  if (!open) {return null;}
  return <PegasusImportDrawerView roots={roots} rootId={rootId} path={path} breadcrumbs={breadcrumbs} directories={directories} directoryCursor={directoryCursor} directoryLoading={directoryLoading} selectedRoot={selectedRoot} step={step} plan={plan} collections={collections} mappings={mappings} availableInstances={availableInstances} activeTags={activeTags} batchTags={batchTags} batchStatus={batchTagStatus} busy={busy} error={error} mapped={mapped} skipped={skipped} taggedCollections={taggedCollections.length} taggedGames={taggedGames} mappedTags={mappedTags} mappingComplete={mappingComplete} onRoot={(id) => {setRootId(id); setPath("");}} onPath={setPath} onMore={() => void loadMoreDirectories()} onBatchTags={(tags) => {setBatchTags(tags); setBatchTagStatus("");}} onApplyBatch={applyBatchTags} onMapping={(id, draft) => setMappings((current) => ({ ...current, [id]: draft }))} onClose={onClose} onScan={() => void scan()} onConfirm={() => void confirmMappings()} onStart={() => void startImport()} onDismissError={() => setError("")} />;
}

export function PegasusImportDetailManager({ initialSummary, initialItems, collections, roots, platformInstances, activeTags = [], initialFilters }: {
  initialSummary: PegasusImportSummary;
  initialItems: PegasusItemList;
  collections: PegasusCollection[];
  roots: ServerImportRoot[];
  platformInstances: PegasusPlatformInstance[];
  activeTags?: TagReference[];
  initialFilters: DetailFilters;
}) {
  const [summary, setSummary] = useState(initialSummary);
  const [items, setItems] = useState(initialItems.items);
  const [nextCursor, setNextCursor] = useState(initialItems.nextCursor);
  const [filters, setFilters] = useState(initialFilters);
  const [draft, setDraft] = useState(initialFilters);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [cancelOpen, setCancelOpen] = useState(false);
  const [mappingOpen, setMappingOpen] = useState(false);

  const requestSummary = useCallback(async () => {
    const { data, response } = await api.GET("/api/v1/admin/pegasus-imports/{pegasusImportId}", { params: { path: { pegasusImportId: initialSummary.id } } });
    if (!data) {throw new Error(await message(response, "任务摘要读取失败"));}
    setSummary(data);
  }, [initialSummary.id]);

  const requestItems = useCallback(async (active: DetailFilters, cursor?: string, append = false) => {
    const { data, response } = await api.GET("/api/v1/admin/pegasus-imports/{pegasusImportId}/items", { params: { path: { pegasusImportId: initialSummary.id }, query: { q: active.query || undefined, outcome: active.outcome || undefined, warning: active.warning || undefined, collectionId: active.collectionId || undefined, cursor, limit: 50 } } });
    if (!data) {throw new Error(await message(response, "任务结果读取失败"));}
    setItems((current) => append ? [...current, ...data.items.filter((item) => !current.some((known) => known.id === item.id))] : data.items);
    setNextCursor(data.nextCursor);
  }, [initialSummary.id]);

  useEffect(() => {
    const values = new URLSearchParams(window.location.search);
    for (const [name, value] of Object.entries({ q: filters.query, outcome: filters.outcome, warning: filters.warning, collectionId: filters.collectionId })) {
      if (value) {values.set(name, value);} else {values.delete(name);}
    }
    const encoded = values.toString();
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${encoded ? `?${encoded}` : ""}`);
  }, [filters]);

  useEffect(() => {
    if (!["SCANNING", "QUEUED", "RUNNING", "CANCEL_REQUESTED"].includes(summary.state)) {return;}
    const update = () => {void requestSummary().catch(() => undefined); void requestItems(filters).catch(() => undefined);};
    const timer = window.setInterval(update, 4_000);
    const jobId = summary.importJobId ?? summary.scanJobId;
    const source = typeof EventSource === "undefined" ? null : new EventSource(`/api/v1/admin/jobs/${encodeURIComponent(jobId)}/events`, { withCredentials: true });
    for (const event of ["progress", "succeeded", "failed", "cancelled"]) {source?.addEventListener(event, update);}
    return () => {window.clearInterval(timer); source?.close();};
  }, [filters, requestItems, requestSummary, summary.importJobId, summary.scanJobId, summary.state]);

  async function applyFilters() {
    setBusy(true); setError("");
    try {setFilters(draft); await requestItems(draft);}
    catch (caught) {setError(caught instanceof Error ? caught.message : "任务结果读取失败");}
    finally {setBusy(false);}
  }

  async function cancel() {
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/pegasus-imports/{pegasusImportId}/cancel", { params: { path: { pegasusImportId: summary.id }, header: { ...writeHeaders(), "If-Match": `"v${summary.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } }, body: { reason: "管理员停止 Pegasus ROM 导入" } });
      if (!data) {throw new Error(await message(response, "取消任务失败"));}
      setSummary(data); setCancelOpen(false);
    } catch (caught) {setError(caught instanceof Error ? caught.message : "取消任务失败");}
    finally {setBusy(false);}
  }

  async function retry() {
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/pegasus-imports/{pegasusImportId}/retry", { params: { path: { pegasusImportId: summary.id }, header: { ...writeHeaders(), "If-Match": `"v${summary.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } }, body: {} });
      if (!data) {throw new Error(await message(response, "重试任务失败"));}
      setSummary(data);
    } catch (caught) {setError(caught instanceof Error ? caught.message : "重试任务失败");}
    finally {setBusy(false);}
  }

  function loadMore() {
    setBusy(true);
    void requestItems(filters, nextCursor ?? undefined, true)
      .catch((caught: unknown) => setError(caught instanceof Error ? caught.message : "加载失败"))
      .finally(() => setBusy(false));
  }

  const mappingDrawer = <PegasusImportDrawer open roots={roots} platformInstances={platformInstances} activeTags={activeTags} resumablePlan={summary} onClose={() => setMappingOpen(false)} onStarted={setSummary} />;
  return <PegasusImportDetailView summary={summary} items={items} nextCursor={nextCursor} draft={draft} collections={collections} busy={busy} error={error} cancelOpen={cancelOpen} mappingOpen={mappingOpen} mappingDrawer={mappingDrawer} onDraft={setDraft} onApplyFilters={() => void applyFilters()} onCancelOpen={setCancelOpen} onCancel={() => void cancel()} onRetry={() => void retry()} onMappingOpen={setMappingOpen} onLoadMore={loadMore} onDismissError={() => setError("")} />;
}
