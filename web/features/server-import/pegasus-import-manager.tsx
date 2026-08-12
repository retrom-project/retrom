"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast } from "@/components/flash-toast";
import { StatusBadge } from "@/components/ui";
import { api, writeHeaders } from "@/lib/api/client";
import type { components } from "@/lib/api/generated/schema";
import { formatBytes } from "@/lib/backend";
import { newUuid } from "@/lib/crypto";
import { responseError } from "@/lib/upload";
import type { ServerImportRoot } from "./server-import-manager";

export type PegasusImportSummary = components["schemas"]["PegasusImportSummary"];
export type PegasusImportList = components["schemas"]["PegasusImportList"];
export type PegasusCollection = components["schemas"]["PegasusSourceCollection"];
export type PegasusItemList = components["schemas"]["PegasusItemList"];
export type PegasusItem = components["schemas"]["PegasusItem"];
type Directory = components["schemas"]["ServerImportDirectory"];

export type PegasusPlatformInstance = {
  id: string;
  name: string;
  platformName: string;
  defaultCoreId: string;
  defaultCoreName: string;
  enabled: boolean;
};

export const pegasusStateLabels: Record<PegasusImportSummary["state"], string> = {
  SCANNING: "正在扫描", AWAITING_MAPPING: "等待映射", QUEUED: "等待导入", RUNNING: "正在导入",
  PARTIAL_FAILURE: "部分失败", COMPLETED: "已完成", CANCEL_REQUESTED: "正在取消", CANCELLED: "已取消",
  FAILED: "任务失败", EXPIRED: "计划已过期",
};

const phaseLabels: Record<NonNullable<PegasusImportSummary["phase"]>, string> = {
  DISCOVERING_METADATA: "发现 metadata", PARSING_METADATA: "解析 metadata", RESOLVING_SOURCES: "核对源文件",
  COPYING_CONTENT: "复制内容", VALIDATING: "运行检查", PUBLISHING: "逐项发布",
};

const outcomeLabels: Record<PegasusItem["executionState"], string> = {
  PENDING: "等待处理", COPYING: "复制内容", VALIDATING: "运行检查", PUBLISHING: "正在发布", PUBLISHED: "已发布",
  SKIPPED_EXISTING: "内容已存在", SKIPPED_MAPPING: "集合已跳过", BLOCKED_SOURCE: "源文件阻断", BLOCKED_CONTENT: "内容阻断",
  BLOCKED_VALIDATION: "运行检查阻断", SOURCE_CHANGED: "源文件已变化", READ_FAILED: "读取失败", COMMIT_FAILED: "提交失败", CANCELLED: "已取消",
};

export function pegasusStateTone(state: PegasusImportSummary["state"]): "good" | "warn" | "bad" | "info" {
  if (state === "COMPLETED") return "good";
  if (state === "FAILED" || state === "PARTIAL_FAILURE") return "bad";
  if (state === "CANCELLED" || state === "CANCEL_REQUESTED" || state === "EXPIRED") return "warn";
  return "info";
}

function message(response: Response, fallback: string) {
  return responseError(response, fallback);
}

type MappingDraft = { action: "" | "IMPORT" | "SKIP"; platformInstanceId: string };

export function PegasusImportDrawer({ open, roots, platformInstances, resumablePlan, onClose, onStarted }: {
  open: boolean;
  roots: ServerImportRoot[];
  platformInstances: PegasusPlatformInstance[];
  resumablePlan?: PegasusImportSummary;
  onClose: () => void;
  onStarted: (summary: PegasusImportSummary) => void;
}) {
  const router = useRouter();
  const drawer = useRef<HTMLElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const [step, setStep] = useState<1 | 2 | 3>(resumablePlan ? 2 : 1);
  const [rootId, setRootId] = useState(resumablePlan?.root.id ?? roots.find((root) => root.status === "AVAILABLE")?.id ?? "");
  const [path, setPath] = useState(resumablePlan?.sourceRelativePath ?? "");
  const [directories, setDirectories] = useState<Directory[]>([]);
  const [directoryCursor, setDirectoryCursor] = useState<string | null>(null);
  const [directoryLoading, setDirectoryLoading] = useState(false);
  const [plan, setPlan] = useState<PegasusImportSummary | null>(resumablePlan ?? null);
  const [collections, setCollections] = useState<PegasusCollection[]>([]);
  const [mappings, setMappings] = useState<Record<string, MappingDraft>>({});
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
      if (!data) throw new Error(await message(response, "Pegasus 集合读取失败"));
      all.push(...data.items);
      cursor = data.nextCursor ?? undefined;
    } while (cursor);
    setCollections(all);
    setMappings(Object.fromEntries(all.map((collection) => [collection.id, {
      action: collection.mappingAction ?? "",
      platformInstanceId: collection.targetPlatformInstanceId ?? "",
    }])));
  }, []);

  const refreshPlan = useCallback(async (planId: string) => {
    const { data, response } = await api.GET("/api/v1/admin/pegasus-imports/{pegasusImportId}", { params: { path: { pegasusImportId: planId } } });
    if (!data) throw new Error(await message(response, "Pegasus 计划读取失败"));
    setPlan(data);
    if (data.state === "AWAITING_MAPPING") await loadCollections(data.id);
    return data;
  }, [loadCollections]);

  useEffect(() => {
    if (!open) return;
    let active = true;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    closeButton.current?.focus();
    if (resumablePlan) {
      queueMicrotask(() => {
        if (active) void refreshPlan(resumablePlan.id).catch((caught: unknown) => setError(caught instanceof Error ? caught.message : "Pegasus 计划读取失败"));
      });
    }
    return () => { active = false; previous?.focus(); };
  }, [open, refreshPlan, resumablePlan]);

  useEffect(() => {
    if (!open || step !== 1 || !rootId) return;
    const controller = new AbortController();
    queueMicrotask(() => { if (!controller.signal.aborted) setDirectoryLoading(true); });
    void api.GET("/api/v1/admin/server-import-roots/{rootId}/directories", {
      params: { path: { rootId }, query: { path, limit: 100 } }, signal: controller.signal,
    }).then(async ({ data, response }) => {
      if (!data) throw new Error(await message(response, "服务器目录读取失败"));
      setDirectories(data.items); setDirectoryCursor(data.nextCursor);
    }).catch((caught: unknown) => {
      if (!(caught instanceof DOMException && caught.name === "AbortError")) setError(caught instanceof Error ? caught.message : "服务器目录读取失败");
    }).finally(() => setDirectoryLoading(false));
    return () => controller.abort();
  }, [open, path, rootId, step]);

  useEffect(() => {
    if (!open || !plan || plan.state !== "SCANNING") return;
    const update = () => void refreshPlan(plan.id).catch((caught: unknown) => setError(caught instanceof Error ? caught.message : "扫描进度读取失败"));
    const timer = window.setInterval(update, 2_000);
    const source = typeof EventSource === "undefined" ? null : new EventSource(`/api/v1/admin/jobs/${encodeURIComponent(plan.scanJobId)}/events`, { withCredentials: true });
    source?.addEventListener("progress", update);
    source?.addEventListener("succeeded", update);
    source?.addEventListener("failed", update);
    return () => { window.clearInterval(timer); source?.close(); };
  }, [open, plan, refreshPlan]);

  async function loadMoreDirectories() {
    if (!directoryCursor || directoryLoading) return;
    setDirectoryLoading(true); setError("");
    try {
      const { data, response } = await api.GET("/api/v1/admin/server-import-roots/{rootId}/directories", { params: { path: { rootId }, query: { path, cursor: directoryCursor, limit: 100 } } });
      if (!data) throw new Error(await message(response, "服务器目录读取失败"));
      setDirectories((current) => [...current, ...data.items.filter((item) => !current.some((known) => known.relativePath === item.relativePath))]);
      setDirectoryCursor(data.nextCursor);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "服务器目录读取失败"); }
    finally { setDirectoryLoading(false); }
  }

  async function scan() {
    if (!rootId || selectedRoot?.status !== "AVAILABLE") return;
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/pegasus-imports", {
        params: { header: { ...writeHeaders(), "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } },
        body: { rootId, sourceRelativePath: path },
      });
      if (!data) throw new Error(await message(response, "Pegasus 扫描创建失败"));
      setPlan(data); setStep(2); onStarted(data);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "Pegasus 扫描创建失败"); }
    finally { setBusy(false); }
  }

  async function confirmMappings() {
    if (!plan || collections.some((collection) => !mappings[collection.id]?.action) || collections.some((collection) => mappings[collection.id]?.action === "IMPORT" && !mappings[collection.id]?.platformInstanceId)) return;
    setBusy(true); setError("");
    try {
      let current = plan;
      const values = collections.map((collection) => {
        const draft = mappings[collection.id];
        return draft.action === "SKIP" ? { collectionId: collection.id, action: "SKIP" as const } : { collectionId: collection.id, action: "IMPORT" as const, platformInstanceId: draft.platformInstanceId };
      });
      for (let offset = 0; offset < values.length; offset += 100) {
        const { data, response } = await api.PUT("/api/v1/admin/pegasus-imports/{pegasusImportId}/collection-mappings", {
          params: { path: { pegasusImportId: current.id }, header: { ...writeHeaders(), "If-Match": `"v${current.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } },
          body: { mappings: values.slice(offset, offset + 100) },
        });
        if (!data) throw new Error(await message(response, "集合映射保存失败"));
        current = data;
      }
      setPlan(current); setStep(3);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "集合映射保存失败"); }
    finally { setBusy(false); }
  }

  async function startImport() {
    if (!plan) return;
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/pegasus-imports/{pegasusImportId}/start", {
        params: { path: { pegasusImportId: plan.id }, header: { ...writeHeaders(), "If-Match": `"v${plan.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } },
        body: { version: plan.version },
      });
      if (!data) throw new Error(await message(response, "Pegasus 导入启动失败"));
      onStarted(data); onClose(); router.push(`/admin/imports/server/pegasus/${data.id}`);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "Pegasus 导入启动失败"); }
    finally { setBusy(false); }
  }

  const mapped = Object.values(mappings).filter((mapping) => mapping.action === "IMPORT").length;
  const skipped = Object.values(mappings).filter((mapping) => mapping.action === "SKIP").length;
  const mappingComplete = collections.length > 0 && mapped + skipped === collections.length && collections.every((collection) => mappings[collection.id]?.action !== "IMPORT" || Boolean(mappings[collection.id]?.platformInstanceId));
  if (!open) return null;
  return <><button type="button" className="runtime-drawer-backdrop" aria-label="关闭 Pegasus 导入" disabled={busy} onClick={onClose} /><aside ref={drawer} className="runtime-drawer server-import-drawer pegasus-import-drawer" role="dialog" aria-modal="true" aria-labelledby="pegasus-import-title" onKeyDown={(event) => {
    if (event.key === "Escape" && !busy) { event.preventDefault(); onClose(); }
    if (event.key !== "Tab") return;
    const focusable = Array.from(drawer.current?.querySelectorAll<HTMLElement>("button:not(:disabled),input:not(:disabled),select:not(:disabled),a[href],[tabindex]:not([tabindex='-1'])") ?? []);
    if (!focusable.length) return;
    const first = focusable[0]; const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  }}><header><div><StatusBadge tone="info">Pegasus ROM</StatusBadge><h2 id="pegasus-import-title">从 Pegasus 目录导入游戏</h2><p>只显示允许 root 内的相对目录；扫描不会复制 ROM 或创建游戏。</p></div><button ref={closeButton} type="button" className="runtime-drawer-close" aria-label="关闭" disabled={busy} onClick={onClose}><AppIcon name="x" /></button></header>
    <ol className="pegasus-stepper" aria-label="导入步骤"><li className={step === 1 ? "is-active" : step > 1 ? "is-complete" : ""}><span>1</span>选择目录</li><li className={step === 2 ? "is-active" : step > 2 ? "is-complete" : ""}><span>2</span>检查与映射</li><li className={step === 3 ? "is-active" : ""}><span>3</span>确认导入</li></ol>
    <div className="runtime-drawer-body">
      {step === 1 ? <><fieldset className="server-root-options"><legend>服务器位置</legend>{roots.map((root) => <label key={root.id}><input type="radio" name="pegasus-root" checked={rootId === root.id} disabled={busy || root.status !== "AVAILABLE"} onChange={() => { setRootId(root.id); setPath(""); }} /><span><strong>{root.label}</strong><small>{root.status === "AVAILABLE" ? "可用" : "不可用"}</small></span></label>)}</fieldset><div className="server-directory-browser"><nav aria-label="当前目录"><button type="button" onClick={() => setPath("")} disabled={!path || busy}>根目录</button>{breadcrumbs.map((part, index) => <button type="button" key={`${part}-${index}`} disabled={index === breadcrumbs.length - 1 || busy} onClick={() => setPath(breadcrumbs.slice(0, index + 1).join("/"))}>/ {part}</button>)}</nav>{directories.length ? <><ul>{directories.map((directory) => <li key={directory.relativePath}><button type="button" disabled={busy} onClick={() => setPath(directory.relativePath)}><AppIcon name="folder" /><span>{directory.name}</span><span aria-hidden="true">→</span></button></li>)}</ul>{directoryCursor ? <button type="button" className="button secondary compact" disabled={directoryLoading || busy} onClick={() => void loadMoreDirectories()}>{directoryLoading ? "正在读取…" : "加载更多目录"}</button> : null}</> : <p role="status">{directoryLoading ? "正在读取子目录…" : "当前目录没有可进入的子目录。"}</p>}</div><div className="server-import-selection-summary"><strong>{selectedRoot?.label ?? "未选择"} / {path || "根目录"}</strong><span>先异步读取 metadata、文件大小与稳定 facts；确认映射后才读取完整 ROM bytes。</span></div></> : null}
      {step === 2 ? <>{plan?.state === "SCANNING" ? <div className="pegasus-scan-progress" aria-live="polite"><span className="button-spinner" /><h3>{plan.phase ? phaseLabels[plan.phase] : "扫描准备中"}</h3><p>任务离开页面后仍会继续。当前发现 {plan.counts.metadata} 个 metadata、{plan.counts.collections} 个集合、{plan.counts.games} 个游戏。</p></div> : null}{plan?.state === "FAILED" ? <div className="runtime-inline-empty"><h3>扫描未完成</h3><p>{plan.lastErrorCode ?? "扫描任务失败"}</p></div> : null}{plan?.state === "AWAITING_MAPPING" ? <><div className="pegasus-scan-summary"><div><span>Metadata</span><strong>{plan.counts.metadata}</strong></div><div><span>Collection</span><strong>{plan.counts.collections}</strong></div><div><span>Game</span><strong>{plan.counts.games}</strong></div><div><span>发现视频</span><strong>{plan.counts.videos}</strong></div></div><p className="pegasus-mapping-note">每个 source collection 必须明确选择游戏目录或跳过；Retrom 不会根据名称、扩展名或 launch 命令猜测。</p><div className="pegasus-collection-list">{collections.map((collection) => <article key={collection.id}><div><h3>{collection.name}</h3><p>{collection.metadataRelativePath} · segment {collection.segmentOrdinal + 1}</p><small>{collection.shortName ? `shortname: ${collection.shortName} · ` : ""}{collection.gameCount} 个游戏 · {collection.issueCount} 个阻断/问题</small></div><label><span>处理方式</span><select aria-label={`${collection.name} 处理方式`} value={mappings[collection.id]?.action === "SKIP" ? "SKIP" : mappings[collection.id]?.platformInstanceId ? `IMPORT:${mappings[collection.id].platformInstanceId}` : ""} onChange={(event) => { const value = event.target.value; setMappings((current) => ({ ...current, [collection.id]: value === "SKIP" ? { action: "SKIP", platformInstanceId: "" } : value.startsWith("IMPORT:") ? { action: "IMPORT", platformInstanceId: value.slice(7) } : { action: "", platformInstanceId: "" } })); }}><option value="">请选择，不会自动映射</option><option value="SKIP">跳过此集合</option>{availableInstances.map((instance) => <option value={`IMPORT:${instance.id}`} key={instance.id}>导入到 {instance.name} · {instance.defaultCoreName}</option>)}</select></label></article>)}</div></> : null}</> : null}
      {step === 3 && plan ? <><div className="pegasus-review-table"><div><span>来源</span><strong>{plan.root.label} / {plan.sourceRelativePath || "根目录"}</strong></div><div><span>映射</span><strong>{mapped} 个导入 · {skipped} 个跳过</strong></div><div><span>可处理 / 阻断</span><strong>{plan.counts.processable} / {plan.counts.blocked} 个游戏</strong></div><div><span>封面 / 视频</span><strong>{plan.counts.covers} / {plan.counts.videos}</strong></div><div><span>预计最多读取</span><strong>{formatBytes(plan.counts.estimatedSourceBytes)}</strong></div><div><span>发布方式</span><strong>运行检查通过后逐项自动发布</strong></div></div><p className="pegasus-mapping-note">开始时会重新核对 metadata digest 与源文件 facts。取消不会回滚已经发布的游戏；阻断和媒体警告会保留在任务详情。</p></> : null}
    </div><footer><button type="button" className="button secondary" disabled={busy} onClick={onClose}>关闭</button>{step === 1 ? <button type="button" className="button" disabled={busy || !rootId || selectedRoot?.status !== "AVAILABLE"} onClick={() => void scan()}>{busy ? "正在创建…" : "扫描此目录"}</button> : null}{step === 2 && plan?.state === "AWAITING_MAPPING" ? <button type="button" className="button" disabled={busy || !mappingComplete} onClick={() => void confirmMappings()}>{busy ? "正在保存…" : "确认映射"}</button> : null}{step === 3 ? <button type="button" className="button" disabled={busy} onClick={() => void startImport()}>{busy ? "正在启动…" : "开始异步导入"}</button> : null}</footer></aside><Toast toast={error ? { message: error, tone: "bad" } : null} onDismiss={() => setError("")} /></>;
}

type DetailFilters = { query: string; outcome: string; warning: string; collectionId: string };

export function PegasusImportDetailManager({ initialSummary, initialItems, collections, initialFilters }: {
  initialSummary: PegasusImportSummary;
  initialItems: PegasusItemList;
  collections: PegasusCollection[];
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

  const requestSummary = useCallback(async () => {
    const { data, response } = await api.GET("/api/v1/admin/pegasus-imports/{pegasusImportId}", { params: { path: { pegasusImportId: initialSummary.id } } });
    if (!data) throw new Error(await message(response, "任务摘要读取失败"));
    setSummary(data);
  }, [initialSummary.id]);

  const requestItems = useCallback(async (active: DetailFilters, cursor?: string, append = false) => {
    const { data, response } = await api.GET("/api/v1/admin/pegasus-imports/{pegasusImportId}/items", { params: { path: { pegasusImportId: initialSummary.id }, query: { q: active.query || undefined, outcome: active.outcome || undefined, warning: active.warning || undefined, collectionId: active.collectionId || undefined, cursor, limit: 50 } } });
    if (!data) throw new Error(await message(response, "任务结果读取失败"));
    setItems((current) => append ? [...current, ...data.items.filter((item) => !current.some((known) => known.id === item.id))] : data.items);
    setNextCursor(data.nextCursor);
  }, [initialSummary.id]);

  useEffect(() => {
    const values = new URLSearchParams(window.location.search);
    for (const [name, value] of Object.entries({ q: filters.query, outcome: filters.outcome, warning: filters.warning, collectionId: filters.collectionId })) {
      if (value) values.set(name, value); else values.delete(name);
    }
    const encoded = values.toString();
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${encoded ? `?${encoded}` : ""}`);
  }, [filters]);

  useEffect(() => {
    if (!["SCANNING", "QUEUED", "RUNNING", "CANCEL_REQUESTED"].includes(summary.state)) return;
    const update = () => { void requestSummary().catch(() => undefined); void requestItems(filters).catch(() => undefined); };
    const timer = window.setInterval(update, 4_000);
    const jobId = summary.importJobId ?? summary.scanJobId;
    const source = typeof EventSource === "undefined" ? null : new EventSource(`/api/v1/admin/jobs/${encodeURIComponent(jobId)}/events`, { withCredentials: true });
    for (const event of ["progress", "succeeded", "failed", "cancelled"]) source?.addEventListener(event, update);
    return () => { window.clearInterval(timer); source?.close(); };
  }, [filters, requestItems, requestSummary, summary.importJobId, summary.scanJobId, summary.state]);

  async function applyFilters() {
    setBusy(true); setError("");
    try { setFilters(draft); await requestItems(draft); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "任务结果读取失败"); }
    finally { setBusy(false); }
  }

  async function cancel() {
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/pegasus-imports/{pegasusImportId}/cancel", { params: { path: { pegasusImportId: summary.id }, header: { ...writeHeaders(), "If-Match": `"v${summary.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } }, body: { reason: "管理员停止 Pegasus ROM 导入" } });
      if (!data) throw new Error(await message(response, "取消任务失败"));
      setSummary(data); setCancelOpen(false);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "取消任务失败"); }
    finally { setBusy(false); }
  }

  async function retry() {
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/pegasus-imports/{pegasusImportId}/retry", { params: { path: { pegasusImportId: summary.id }, header: { ...writeHeaders(), "If-Match": `"v${summary.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } }, body: {} });
      if (!data) throw new Error(await message(response, "重试任务失败"));
      setSummary(data);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "重试任务失败"); }
    finally { setBusy(false); }
  }

  const terminal = ["COMPLETED", "PARTIAL_FAILURE", "FAILED", "CANCELLED", "EXPIRED"].includes(summary.state);
  return <div className="server-import-detail-page pegasus-detail-page">
    <section className="server-import-detail-head panel"><div><StatusBadge tone={pegasusStateTone(summary.state)}>{pegasusStateLabels[summary.state]}</StatusBadge><h2>{summary.root.label} / {summary.sourceRelativePath || "根目录"}</h2><p aria-live="polite">{summary.phase ? phaseLabels[summary.phase] : terminal ? "任务已结束" : "等待处理"}</p></div><div>{["SCANNING", "QUEUED", "RUNNING"].includes(summary.state) ? <button type="button" className="button secondary" disabled={busy} onClick={() => setCancelOpen(true)}>取消任务</button> : null}{summary.retryable ? <button type="button" className="button" disabled={busy} onClick={() => void retry()}>重试失败项</button> : null}<Link href="/admin/imports/server?action=pegasus" className="button secondary">新建 Pegasus 导入</Link></div></section>
    <section className="runtime-kpis" aria-label="Pegasus 导入摘要"><article><small>Collection</small><strong>{summary.counts.collections}</strong><p>{summary.counts.mappedCollections} 导入 · {summary.counts.skippedCollections} 跳过</p></article><article><small>游戏</small><strong>{summary.counts.games}</strong><p>{summary.counts.processable} 项可处理</p></article><article className="has-success"><small>已发布 / 已存在</small><strong>{summary.counts.published} / {summary.counts.existing}</strong><p>逐项完成，不回滚成功项</p></article><article className={summary.counts.blocked + summary.counts.failed ? "has-danger" : ""}><small>阻断 / 失败</small><strong>{summary.counts.blocked} / {summary.counts.failed}</strong><p>{summary.counts.mediaWarnings} 个媒体警告</p></article></section>
    {summary.lastErrorCode ? <p className="server-import-error panel"><strong>{summary.lastErrorCode}</strong><span>外部 source 不属于备份；目录变化时请按结果提示重扫或重试。</span></p> : null}
    <section className="server-import-results"><div className="runtime-section-heading"><div><h2>游戏处理结果</h2><p>标题、映射、媒体状态和最终 outcome 均来自冻结的任务证据。</p></div><span>{items.length} / {summary.counts.games} 项</span></div>
      <form className="server-import-result-filters panel pegasus-result-filters" onSubmit={(event) => { event.preventDefault(); void applyFilters(); }}><label><span>搜索标题</span><input type="search" value={draft.query} onChange={(event) => setDraft((current) => ({ ...current, query: event.target.value }))} /></label><label><span>结果</span><select value={draft.outcome} onChange={(event) => setDraft((current) => ({ ...current, outcome: event.target.value }))}><option value="">全部结果</option>{Object.entries(outcomeLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label><label><span>媒体警告</span><input value={draft.warning} placeholder="例如 PEGASUS_VIDEO_UNSUPPORTED" onChange={(event) => setDraft((current) => ({ ...current, warning: event.target.value }))} /></label><label><span>Collection</span><select value={draft.collectionId} onChange={(event) => setDraft((current) => ({ ...current, collectionId: event.target.value }))}><option value="">全部 Collection</option>{collections.map((collection) => <option value={collection.id} key={collection.id}>{collection.name}</option>)}</select></label><button type="submit" className="button secondary compact" disabled={busy}>{busy ? "正在筛选…" : "应用筛选"}</button></form>
      <div className="pegasus-result-table" role="table" aria-label="Pegasus 导入结果">{items.map((item) => <article role="row" key={item.id}><div role="cell"><h3>{item.title}</h3><p>{item.collectionName ?? "无有效 Collection"} → {item.targetPlatformInstanceName ?? "未映射"}</p><small>{item.metadataRelativePath} · {item.contentKind ?? "内容类型待定"}</small></div><div role="cell" className="pegasus-result-media"><StatusBadge tone={item.media.cover === "READY" ? "good" : item.media.cover === "WARNING" ? "warn" : "info"}>封面 {item.media.cover}</StatusBadge><StatusBadge tone={item.media.video === "READY" ? "good" : item.media.video === "WARNING" ? "warn" : "info"}>视频 {item.media.video}</StatusBadge></div><div role="cell"><StatusBadge tone={item.executionState === "PUBLISHED" ? "good" : item.executionState.startsWith("BLOCKED") || ["SOURCE_CHANGED", "READ_FAILED", "COMMIT_FAILED"].includes(item.executionState) ? "bad" : "warn"}>{outcomeLabels[item.executionState]}</StatusBadge><small>{item.errorCode ?? item.discoveryCode ?? (item.warnings.map((warning) => warning.code).join("、") || "无附加结果码")}</small></div><div role="cell">{item.publishedGameId ? <Link href={`/games/${item.publishedGameId}`}>查看游戏 →</Link> : item.existingGameId ? <Link href={`/games/${item.existingGameId}`}>已有游戏 →</Link> : item.discoveryCode === "PEGASUS_MULTIPLE_LAUNCH_FILES_UNSUPPORTED" ? <small>Pegasus 把多个文件视为可选启动项；请整理为单文件或受支持的 Saturn M3U。</small> : <span>—</span>}</div></article>)}</div>
      {nextCursor ? <button type="button" className="button secondary server-import-history-more" disabled={busy} onClick={() => { setBusy(true); void requestItems(filters, nextCursor, true).catch((caught: unknown) => setError(caught instanceof Error ? caught.message : "加载失败")).finally(() => setBusy(false)); }}>加载更多结果</button> : null}
    </section>
    <ConfirmDialog open={cancelOpen} title="取消这次 Pegasus 导入？" description="已经发布的游戏不会回滚，尚未处理的项目会在安全检查点停止。" confirmLabel="确认取消" tone="danger" busy={busy} onCancel={() => setCancelOpen(false)} onConfirm={() => void cancel()} />
    <Toast toast={error ? { message: error, tone: "bad" } : null} onDismiss={() => setError("")} />
  </div>;
}
