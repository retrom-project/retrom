"use client";

import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState, type DragEvent, type FormEvent } from "react";
import { type ToastMessage } from "@/components/flash-toast";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import {
  canReorderPlatformDirectories,
  filterPlatformDirectories,
  platformDirectorySummary,
  summarizeRecommendations,
  type Platform,
  type PlatformDirectoryFilters,
  type PlatformInstance,
  type PlatformRecommendations,
  type PlatformRecommendationsApplyResult,
} from "./platform-directory-list";
import { PlatformManagerView } from "./platform-manager-view";

export type { Platform, PlatformInstance, PlatformRecommendations } from "./platform-directory-list";

export type PendingAction =
  | { kind: "core"; instance: PlatformInstance; coreId: string; coreName: string; impactDigest: string; counts: { ready: number; needsValidation: number; blocked: number } }
  | { kind: "delete"; instance: PlatformInstance };

export type EditTarget = { id: string; field: "name" | "description" } | null;

const initialFilters: PlatformDirectoryFilters = { query: "", platformId: "", status: "ALL", sort: "ORDER" };

async function message(response: Response) {
  const body = await response.json().catch(() => null) as { error?: { message?: string } } | null;
  return body?.error?.message ?? `请求失败（${response.status}）`;
}

export function PlatformManager({ instances, platforms, recommendations = null, createOpen }: { instances: PlatformInstance[]; platforms: Platform[]; recommendations?: PlatformRecommendations | null; createOpen: boolean }) {
  const router = useRouter();
  const [rows, setRows] = useState(() => [...instances].sort((left, right) => left.sortOrder - right.sortOrder || left.id.localeCompare(right.id)));
  const [filters, setFilters] = useState(initialFilters);
  const [busy, setBusy] = useState<string | null>(null);
  const [toast, setToast] = useState<ToastMessage | null>(null);
  const [recommendationState, setRecommendationState] = useState(recommendations);
  const [pending, setPending] = useState<PendingAction | null>(null);
  const [editing, setEditing] = useState<EditTarget>(null);
  const [draggedId, setDraggedId] = useState<string | null>(null);
  const [openMenuId, setOpenMenuId] = useState<string | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(createOpen);
  const [sortHelpOpen, setSortHelpOpen] = useState(false);
  const enabledPlatforms = useMemo(() => platforms.filter((platform) => platform.enabled), [platforms]);
  const initialPlatform = enabledPlatforms[0];
  const [createPlatformID, setCreatePlatformID] = useState(initialPlatform?.id ?? "");
  const [createCoreID, setCreateCoreID] = useState(initialPlatform?.cores.find((core) => core.enabled)?.id ?? "");
  const [createName, setCreateName] = useState("");
  const [createDescription, setCreateDescription] = useState("");
  const busyRef = useRef(busy);

  const visibleRows = useMemo(() => filterPlatformDirectories(rows, filters), [rows, filters]);
  const summary = useMemo(() => platformDirectorySummary(rows), [rows]);
  const reorderEnabled = canReorderPlatformDirectories(filters);
  const selectedCreatePlatform = platforms.find((platform) => platform.id === createPlatformID);
  const selectedCreateCore = selectedCreatePlatform?.cores.find((core) => core.id === createCoreID);

  useEffect(() => {
    if (!openMenuId) {return;}
    const close = (event: PointerEvent) => {
      if (!(event.target instanceof Element) || !event.target.closest(".platform-more-wrap")) {setOpenMenuId(null);}
    };
    document.addEventListener("pointerdown", close);
    return () => document.removeEventListener("pointerdown", close);
  }, [openMenuId]);

  useEffect(() => {
    busyRef.current = busy;
  }, [busy]);

  useEffect(() => {
    if (!drawerOpen) {return;}
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    document.getElementById("platform-drawer-close")?.focus();
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape" && busyRef.current !== "create") {setDrawerOpen(false);}
    };
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener("keydown", closeOnEscape);
      previous?.focus();
    };
  }, [drawerOpen]);

  function clearFeedback() { setToast(null); }

  function selectCreatePlatform(platformId: string) {
    const platform = platforms.find((item) => item.id === platformId);
    setCreatePlatformID(platformId);
    setCreateCoreID(platform?.cores.find((core) => core.enabled)?.id ?? "");
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy("create");
    clearFeedback();
    const sortOrder = (rows.at(-1)?.sortOrder ?? 0) + 100;
    const body = { platformId: createPlatformID, defaultCoreId: createCoreID, name: createName, description: createDescription, sortOrder };
    try {
      const response = await fetch("/api/v1/admin/platform-instances", { method: "POST", headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }), body: JSON.stringify(body) });
      if (!response.ok) {throw new Error(await message(response));}
      setCreateName("");
      setCreateDescription("");
      setDrawerOpen(false);
      router.refresh();
    } catch (caught) { setToast({ message: caught instanceof Error ? caught.message : "目录创建失败", tone: "bad" }); }
    finally { setBusy(null); }
  }

  async function patchInstance(instance: PlatformInstance, body: Partial<Pick<PlatformInstance, "name" | "description" | "enabled">>) {
    setBusy(instance.id);
    clearFeedback();
    try {
      const response = await fetch(`/api/v1/admin/platform-instances/${instance.id}`, { method: "PATCH", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${instance.version}"` }), body: JSON.stringify(body) });
      if (!response.ok) {throw new Error(await message(response));}
      const updated = await response.json() as Partial<PlatformInstance> & { version: number };
      setRows((current) => current.map((row) => row.id === instance.id ? { ...row, ...updated } : row));
      setEditing(null);
    } catch (caught) { setToast({ message: caught instanceof Error ? caught.message : "目录更新失败", tone: "bad" }); }
    finally { setBusy(null); }
  }

  async function submitInline(event: FormEvent<HTMLFormElement>, instance: PlatformInstance, field: "name" | "description") {
    event.preventDefault();
    const value = String(new FormData(event.currentTarget).get(field) ?? "");
    await patchInstance(instance, { [field]: value });
  }

  async function previewCore(instance: PlatformInstance, coreId: string) {
    if (coreId === instance.defaultCoreId) {return;}
    setBusy(instance.id);
    clearFeedback();
    try {
      const headers = await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${instance.version}"` });
      const preview = await fetch(`/api/v1/admin/platform-instances/${instance.id}/default-core-preview`, { method: "POST", headers, body: JSON.stringify({ coreId, cursor: null, limit: 100 }) });
      if (!preview.ok) {throw new Error(await message(preview));}
      const impact = await preview.json() as { impactDigest: string; counts: { ready: number; needsValidation: number; blocked: number } };
      const coreName = platforms.flatMap((platform) => platform.cores).find((core) => core.id === coreId)?.name ?? coreId;
      setPending({ kind: "core", instance, coreId, coreName, impactDigest: impact.impactDigest, counts: impact.counts });
    } catch (caught) { setToast({ message: caught instanceof Error ? caught.message : "无法预览运行方式影响", tone: "bad" }); }
    finally { setBusy(null); }
  }

  async function confirmPending() {
    if (!pending) {return;}
    setBusy(pending.instance.id);
    clearFeedback();
    try {
      if (pending.kind === "core") {
        const headers = await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${pending.instance.version}"`, "Idempotency-Key": newUuid() });
        const response = await fetch(`/api/v1/admin/platform-instances/${pending.instance.id}/default-core`, { method: "POST", headers, body: JSON.stringify({ coreId: pending.coreId, impactDigest: pending.impactDigest, confirmBlocked: pending.counts.blocked > 0 }) });
        if (!response.ok) {throw new Error(await message(response));}
        const updated = await response.json() as { version: number };
        setRows((current) => current.map((row) => row.id === pending.instance.id ? { ...row, defaultCoreId: pending.coreId, defaultCoreName: pending.coreName, version: updated.version } : row));
      } else {
        const response = await fetch(`/api/v1/admin/platform-instances/${pending.instance.id}`, { method: "DELETE", headers: await writeHeaders({ "If-Match": `"v${pending.instance.version}"` }) });
        if (!response.ok) {throw new Error(await message(response));}
        setRows((current) => current.filter((row) => row.id !== pending.instance.id));
      }
      setPending(null);
    } catch (caught) { setToast({ message: caught instanceof Error ? caught.message : "目录操作失败", tone: "bad" }); }
    finally { setBusy(null); }
  }

  async function persistOrder(next: PlatformInstance[], previous: PlatformInstance[]) {
    setBusy("order");
    clearFeedback();
    try {
      const response = await fetch("/api/v1/admin/platform-instances/order", { method: "PUT", headers: await writeHeaders({ "Content-Type": "application/json" }), body: JSON.stringify({ items: next.map((item) => ({ id: item.id, version: item.version })) }) });
      if (!response.ok) {throw new Error(await message(response));}
      const result = await response.json() as { items: Array<{ id: string; sortOrder: number; version: number }> };
      const projections = new Map(result.items.map((item) => [item.id, item]));
      setRows((current) => current.map((item) => ({ ...item, ...projections.get(item.id) })));
    } catch (caught) {
      setRows(previous);
      setToast({ message: caught instanceof Error ? caught.message : "目录排序失败", tone: "bad" });
    } finally { setBusy(null); }
  }

  function move(instanceId: string, targetIndex: number) {
    if (busy || !reorderEnabled) {return;}
    const previous = [...rows];
    const sourceIndex = rows.findIndex((row) => row.id === instanceId);
    if (sourceIndex < 0) {return;}
    const bounded = Math.max(0, Math.min(rows.length - 1, targetIndex));
    if (sourceIndex === bounded) {return;}
    const next = [...rows];
    const [moved] = next.splice(sourceIndex, 1);
    next.splice(bounded, 0, moved);
    setRows(next);
    void persistOrder(next, previous);
  }

  function dropOn(event: DragEvent<HTMLDivElement>, targetId: string) {
    event.preventDefault();
    const sourceId = draggedId;
    setDraggedId(null);
    if (!sourceId || sourceId === targetId || !reorderEnabled) {return;}
    move(sourceId, rows.findIndex((row) => row.id === targetId));
  }

  function startDrag(event: DragEvent<HTMLButtonElement>, instanceId: string) {
    const row = event.currentTarget.closest<HTMLElement>(".platform-directory-row");
    if (row) {
      const bounds = row.getBoundingClientRect();
      const offsetX = Math.max(0, Math.min(bounds.width, event.clientX - bounds.left));
      const offsetY = Math.max(0, Math.min(bounds.height, event.clientY - bounds.top));
      event.dataTransfer.effectAllowed = "move";
      event.dataTransfer.setDragImage(row, offsetX, offsetY);
    }
    setDraggedId(instanceId);
  }

  async function applyRecommendations() {
    if (!recommendationState || recommendationState.summary.missingCount === 0 || busy) {return;}
    setBusy("recommendations");
    clearFeedback();
    try {
      const response = await fetch("/api/v1/admin/platform-instances/recommendations/apply", {
        method: "POST",
        credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }),
        body: "{}",
      });
      if (!response.ok) {throw new Error(await message(response));}
      const result = await response.json() as PlatformRecommendationsApplyResult;
      const created: PlatformInstance[] = result.created;
      setRows((current) => {
        const existing = new Set(current.map((item) => item.id));
        return [...current, ...created.filter((item) => !existing.has(item.id))]
          .sort((left, right) => left.sortOrder - right.sortOrder || left.id.localeCompare(right.id));
      });
      setRecommendationState({ catalogVersion: result.catalogVersion, items: result.items, summary: summarizeRecommendations(result.items) });
      const suppressed = result.summary.suppressedCount
		? `；${result.summary.suppressedCount} 个已停用或删除的推荐目录未恢复`
        : "";
      setToast({ message: `已创建 ${result.summary.createdCount} 个；已有 ${result.summary.coveredCount} 个目录保持不变${suppressed}。`, tone: "good" });
      router.refresh();
    } catch (caught) {
      setToast({ message: caught instanceof Error ? caught.message : "补全推荐目录失败。没有创建任何目录，请重试。", tone: "bad" });
    } finally {
      setBusy(null);
    }
  }

  return <PlatformManagerView
    busy={busy} createCoreID={createCoreID} createDescription={createDescription} createName={createName} createPlatformID={createPlatformID}
    draggedId={draggedId} drawerOpen={drawerOpen} editing={editing}
    enabledPlatforms={enabledPlatforms} filters={filters} onApplyRecommendations={() => void applyRecommendations()}
    onConfirmPending={() => void confirmPending()} onCreate={(event) => void create(event)} onCreateCore={setCreateCoreID}
    onCreateDescription={setCreateDescription} onCreateName={setCreateName} onDelete={(instance) => setPending({ kind: "delete", instance })}
    onDragEnd={() => setDraggedId(null)} onDrawer={setDrawerOpen} onDrop={dropOn} onEdit={setEditing}
    onFilters={(patch) => setFilters((current) => ({ ...current, ...patch }))} onMenu={setOpenMenuId} onMove={move}
    onPatch={(instance, body) => void patchInstance(instance, body)} onPendingClose={() => setPending(null)}
    onPreviewCore={(instance, coreId) => void previewCore(instance, coreId)} onSelectCreatePlatform={selectCreatePlatform}
    onSortHelp={setSortHelpOpen} onStartDrag={startDrag} onSubmitInline={(event, instance, field) => void submitInline(event, instance, field)}
    onToastDismiss={() => setToast(null)} openMenuId={openMenuId} pending={pending} platforms={platforms}
    recommendationState={recommendationState} reorderEnabled={reorderEnabled} rows={rows} selectedCreateCore={selectedCreateCore}
    selectedCreatePlatform={selectedCreatePlatform} sortHelpOpen={sortHelpOpen} summary={summary} toast={toast} visibleRows={visibleRows}
  />;
}
