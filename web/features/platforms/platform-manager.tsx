"use client";

import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState, type DragEvent, type FormEvent } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { EmptyState, FeedbackBanner, PageHeader } from "@/components/ui";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import {
  canReorderPlatformDirectories,
  filterPlatformDirectories,
  platformDirectorySummary,
  type Platform,
  type PlatformDirectoryFilters,
  type PlatformInstance,
} from "./platform-directory-list";

export type { Platform, PlatformInstance } from "./platform-directory-list";

type PendingAction =
  | { kind: "core"; instance: PlatformInstance; coreId: string; coreName: string; impactDigest: string; counts: { ready: number; needsValidation: number; blocked: number } }
  | { kind: "delete"; instance: PlatformInstance };

type EditTarget = { id: string; field: "name" | "description" } | null;

const initialFilters: PlatformDirectoryFilters = { query: "", platformId: "", status: "ALL", sort: "ORDER" };

async function message(response: Response) {
  const body = await response.json().catch(() => null) as { error?: { message?: string } } | null;
  return body?.error?.message ?? `请求失败（${response.status}）`;
}

export function PlatformManager({ instances, platforms, createOpen }: { instances: PlatformInstance[]; platforms: Platform[]; createOpen: boolean }) {
  const router = useRouter();
  const [rows, setRows] = useState(() => [...instances].sort((left, right) => left.sortOrder - right.sortOrder || left.id.localeCompare(right.id)));
  const [filters, setFilters] = useState(initialFilters);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState("");
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
  const createTriggerRef = useRef<HTMLButtonElement>(null);
  const drawerCloseRef = useRef<HTMLButtonElement>(null);
  const busyRef = useRef(busy);

  const visibleRows = useMemo(() => filterPlatformDirectories(rows, filters), [rows, filters]);
  const summary = useMemo(() => platformDirectorySummary(rows), [rows]);
  const reorderEnabled = canReorderPlatformDirectories(filters);
  const selectedCreatePlatform = platforms.find((platform) => platform.id === createPlatformID);
  const selectedCreateCore = selectedCreatePlatform?.cores.find((core) => core.id === createCoreID);

  useEffect(() => {
    if (!openMenuId) return;
    const close = (event: PointerEvent) => {
      if (!(event.target instanceof Element) || !event.target.closest(".platform-more-wrap")) setOpenMenuId(null);
    };
    document.addEventListener("pointerdown", close);
    return () => document.removeEventListener("pointerdown", close);
  }, [openMenuId]);

  useEffect(() => {
    busyRef.current = busy;
  }, [busy]);

  useEffect(() => {
    if (!drawerOpen) return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    drawerCloseRef.current?.focus();
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape" && busyRef.current !== "create") setDrawerOpen(false);
    };
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener("keydown", closeOnEscape);
      previous?.focus();
    };
  }, [drawerOpen]);

  function clearFeedback() { setError(""); }

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
      if (!response.ok) throw new Error(await message(response));
      setCreateName("");
      setCreateDescription("");
      setDrawerOpen(false);
      router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "目录创建失败"); }
    finally { setBusy(null); }
  }

  async function patchInstance(instance: PlatformInstance, body: Partial<Pick<PlatformInstance, "name" | "description" | "enabled">>) {
    setBusy(instance.id);
    clearFeedback();
    try {
      const response = await fetch(`/api/v1/admin/platform-instances/${instance.id}`, { method: "PATCH", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${instance.version}"` }), body: JSON.stringify(body) });
      if (!response.ok) throw new Error(await message(response));
      const updated = await response.json() as Partial<PlatformInstance> & { version: number };
      setRows((current) => current.map((row) => row.id === instance.id ? { ...row, ...updated } : row));
      setEditing(null);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "目录更新失败"); }
    finally { setBusy(null); }
  }

  async function submitInline(event: FormEvent<HTMLFormElement>, instance: PlatformInstance, field: "name" | "description") {
    event.preventDefault();
    const value = String(new FormData(event.currentTarget).get(field) ?? "");
    await patchInstance(instance, { [field]: value });
  }

  async function previewCore(instance: PlatformInstance, coreId: string) {
    if (coreId === instance.defaultCoreId) return;
    setBusy(instance.id);
    clearFeedback();
    try {
      const headers = await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${instance.version}"` });
      const preview = await fetch(`/api/v1/admin/platform-instances/${instance.id}/default-core-preview`, { method: "POST", headers, body: JSON.stringify({ coreId, cursor: null, limit: 100 }) });
      if (!preview.ok) throw new Error(await message(preview));
      const impact = await preview.json() as { impactDigest: string; counts: { ready: number; needsValidation: number; blocked: number } };
      const coreName = platforms.flatMap((platform) => platform.cores).find((core) => core.id === coreId)?.name ?? coreId;
      setPending({ kind: "core", instance, coreId, coreName, impactDigest: impact.impactDigest, counts: impact.counts });
    } catch (caught) { setError(caught instanceof Error ? caught.message : "无法预览运行方式影响"); }
    finally { setBusy(null); }
  }

  async function confirmPending() {
    if (!pending) return;
    setBusy(pending.instance.id);
    clearFeedback();
    try {
      if (pending.kind === "core") {
        const headers = await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${pending.instance.version}"`, "Idempotency-Key": newUuid() });
        const response = await fetch(`/api/v1/admin/platform-instances/${pending.instance.id}/default-core`, { method: "POST", headers, body: JSON.stringify({ coreId: pending.coreId, impactDigest: pending.impactDigest, confirmBlocked: pending.counts.blocked > 0 }) });
        if (!response.ok) throw new Error(await message(response));
        const updated = await response.json() as { version: number };
        setRows((current) => current.map((row) => row.id === pending.instance.id ? { ...row, defaultCoreId: pending.coreId, defaultCoreName: pending.coreName, version: updated.version } : row));
      } else {
        const response = await fetch(`/api/v1/admin/platform-instances/${pending.instance.id}`, { method: "DELETE", headers: await writeHeaders({ "If-Match": `"v${pending.instance.version}"` }) });
        if (!response.ok) throw new Error(await message(response));
        setRows((current) => current.filter((row) => row.id !== pending.instance.id));
      }
      setPending(null);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "目录操作失败"); }
    finally { setBusy(null); }
  }

  async function persistOrder(next: PlatformInstance[], previous: PlatformInstance[]) {
    setBusy("order");
    clearFeedback();
    try {
      const response = await fetch("/api/v1/admin/platform-instances/order", { method: "PUT", headers: await writeHeaders({ "Content-Type": "application/json" }), body: JSON.stringify({ items: next.map((item) => ({ id: item.id, version: item.version })) }) });
      if (!response.ok) throw new Error(await message(response));
      const result = await response.json() as { items: Array<{ id: string; sortOrder: number; version: number }> };
      const projections = new Map(result.items.map((item) => [item.id, item]));
      setRows((current) => current.map((item) => ({ ...item, ...projections.get(item.id) })));
    } catch (caught) {
      setRows(previous);
      setError(caught instanceof Error ? caught.message : "目录排序失败");
    } finally { setBusy(null); }
  }

  function move(instanceId: string, targetIndex: number) {
    if (busy || !reorderEnabled) return;
    const previous = [...rows];
    const sourceIndex = rows.findIndex((row) => row.id === instanceId);
    if (sourceIndex < 0) return;
    const bounded = Math.max(0, Math.min(rows.length - 1, targetIndex));
    if (sourceIndex === bounded) return;
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
    if (!sourceId || sourceId === targetId || !reorderEnabled) return;
    move(sourceId, rows.findIndex((row) => row.id === targetId));
  }

  return <div className="platform-directory-manager">
    <PageHeader eyebrow="管理后台" title="游戏目录" description="维护现有游戏平台实例，为每个游戏集合配置推荐运行方式。列表只展示当前状态；修改默认运行方式时，系统会在确认前单独展示影响范围。" actions={<><button className="button secondary" type="button" onClick={() => setSortHelpOpen(true)}>排序说明</button><button ref={createTriggerRef} className="button" type="button" onClick={() => setDrawerOpen(true)}><AppIcon name="plus" />新建游戏目录</button></>} />

    {error ? <div className="platform-directory-error"><FeedbackBanner tone="bad">{error}</FeedbackBanner></div> : null}

    <section className="platform-directory-toolbar" aria-label="筛选游戏目录">
      <label className="platform-directory-search"><span>搜索目录</span><span><AppIcon name="search" /><input type="search" value={filters.query} placeholder="输入目录名称、平台或说明" onChange={(event) => setFilters((current) => ({ ...current, query: event.target.value }))} /></span></label>
      <label><span>游戏平台</span><select value={filters.platformId} onChange={(event) => setFilters((current) => ({ ...current, platformId: event.target.value }))}><option value="">所有平台</option>{platforms.map((platform) => <option value={platform.id} key={platform.id}>{platform.name}</option>)}</select></label>
      <label><span>启用状态</span><select value={filters.status} onChange={(event) => setFilters((current) => ({ ...current, status: event.target.value as PlatformDirectoryFilters["status"] }))}><option value="ALL">全部状态</option><option value="ENABLED">已启用</option><option value="DISABLED">已停用</option></select></label>
      <label><span>排序方式</span><select value={filters.sort} onChange={(event) => setFilters((current) => ({ ...current, sort: event.target.value as PlatformDirectoryFilters["sort"] }))}><option value="ORDER">展示顺序</option><option value="NAME">名称 A-Z</option><option value="GAME_COUNT">游戏数</option></select></label>
    </section>

    <div className="platform-directory-quick">
      <div className="platform-directory-chips" aria-label="游戏目录摘要"><span className="active">全部 {summary.total}</span><span>已启用 {summary.enabled}</span><span>已停用 {summary.disabled}</span><span>空目录 {summary.empty}</span><span>Arcade {summary.arcade}</span></div>
      <p><strong>拖动左侧手柄</strong>调整用户侧目录展示顺序</p>
    </div>

    {visibleRows.length ? <section className="platform-directory-table-scroll" aria-label="游戏目录表格，可横向滚动" tabIndex={0}>
      <div className="platform-directory-table" role="table" aria-label="游戏目录">
        <div role="rowgroup"><div className="platform-directory-table-head" role="row"><span role="columnheader">顺序</span><span role="columnheader">游戏目录</span><span role="columnheader">游戏平台</span><span role="columnheader">游戏数</span><span className="platform-directory-core-head" role="columnheader">推荐运行方式 <span className="platform-directory-info" tabIndex={0}>i<span role="tooltip">更改推荐运行方式时，系统会先检查此目录中现有游戏的兼容性，并在应用前展示影响结果。</span></span></span><span role="columnheader">启用状态</span><span role="columnheader">操作</span></div></div>
        <div role="rowgroup">{visibleRows.map((instance, index) => {
          const globalIndex = rows.findIndex((row) => row.id === instance.id);
          const menuOpen = openMenuId === instance.id;
          return <div className={`platform-directory-row${menuOpen ? " has-open-menu" : ""}`} role="row" key={instance.id} onDragOver={(event) => { if (reorderEnabled) event.preventDefault(); }} onDrop={(event) => dropOn(event, instance.id)}>
            <div className="platform-directory-order" role="cell"><button className="platform-directory-handle" type="button" draggable={reorderEnabled && busy === null} disabled={!reorderEnabled || busy !== null} aria-label={`拖动“${instance.name}”调整顺序`} title={reorderEnabled ? "拖动排序；也可使用上下方向键" : "清除筛选并选择展示顺序后可排序"} onDragStart={() => setDraggedId(instance.id)} onDragEnd={() => setDraggedId(null)} onKeyDown={(event) => { if (event.key === "ArrowUp") { event.preventDefault(); move(instance.id, globalIndex - 1); } if (event.key === "ArrowDown") { event.preventDefault(); move(instance.id, globalIndex + 1); } }}><AppIcon name="grip" /></button><span>{String(globalIndex + 1).padStart(2, "0")}</span></div>
            <div className="platform-directory-copy" role="cell">{editing?.id === instance.id && editing.field === "name" ? <form className="platform-directory-inline" onSubmit={(event) => void submitInline(event, instance, "name")}><input aria-label="游戏目录" name="name" defaultValue={instance.name} required maxLength={200} autoFocus /><button className="button" disabled={busy !== null}>保存</button><button className="icon-button" type="button" aria-label="取消修改目录名称" onClick={() => setEditing(null)}><AppIcon name="x" /></button></form> : <h3>{instance.name}</h3>}{editing?.id === instance.id && editing.field === "description" ? <form className="platform-directory-inline" onSubmit={(event) => void submitInline(event, instance, "description")}><textarea aria-label="给用户看的说明" name="description" defaultValue={instance.description} rows={1} maxLength={10000} autoFocus /><button className="button" disabled={busy !== null}>保存</button><button className="icon-button" type="button" aria-label="取消修改说明" onClick={() => setEditing(null)}><AppIcon name="x" /></button></form> : <p>{instance.description || "暂无说明"}</p>}</div>
            <div className="platform-directory-platform" role="cell"><strong>{instance.platformName}</strong><small>平台实例</small></div>
            <div className="platform-directory-games" role="cell"><strong className={instance.gameCount === 0 ? "empty" : ""}>{instance.gameCount} 款</strong>{instance.gameCount === 0 ? <small>空目录</small> : null}</div>
            <div role="cell"><label className="sr-only" htmlFor={`core-${instance.id}`}>“{instance.name}”的推荐运行方式</label><select id={`core-${instance.id}`} value={instance.defaultCoreId} disabled={busy !== null} onChange={(event) => void previewCore(instance, event.target.value)}>{platforms.find((platform) => platform.id === instance.platformId)?.cores.filter((core) => core.enabled).map((core) => <option value={core.id} key={core.id}>{core.name}</option>)}</select></div>
            <div className="platform-directory-state" role="cell"><label className={`platform-directory-toggle${instance.enabled ? "" : " off"}`} title={instance.enabled ? "取消勾选后，此目录中的游戏将从用户侧隐藏" : "勾选后，此目录中的游戏将重新显示在用户侧"}><input type="checkbox" aria-label={`“${instance.name}”启用状态`} checked={instance.enabled} disabled={busy !== null} onChange={(event) => void patchInstance(instance, { enabled: event.target.checked })} /><span className="box">{instance.enabled ? "✓" : "–"}</span><span>{instance.enabled ? "已启用" : "已停用"}</span></label></div>
            <div className={`platform-more-wrap${index >= visibleRows.length - 2 ? " opens-up" : ""}`} role="cell"><button className="platform-directory-more" type="button" aria-label={`管理目录“${instance.name}”`} aria-expanded={menuOpen} onClick={() => setOpenMenuId(menuOpen ? null : instance.id)}>•••</button>{menuOpen ? <div className="platform-directory-menu" role="menu"><button type="button" role="menuitem" onClick={() => { setEditing({ id: instance.id, field: "name" }); setOpenMenuId(null); }}>编辑名称</button><button type="button" role="menuitem" onClick={() => { setEditing({ id: instance.id, field: "description" }); setOpenMenuId(null); }}>编辑说明</button><button className="danger" type="button" role="menuitem" disabled={instance.gameCount > 0 || busy !== null} onClick={() => { setPending({ kind: "delete", instance }); setOpenMenuId(null); }}>删除{instance.gameCount === 0 ? "空" : ""}目录</button>{instance.gameCount > 0 ? <small>还有 {instance.gameCount} 款游戏，无法删除</small> : null}</div> : null}</div>
          </div>;
        })}</div>
      </div>
    </section> : <EmptyState title={rows.length ? "没有匹配的游戏目录" : "还没有游戏目录"} description={rows.length ? "请调整搜索或筛选条件。" : "点击“新建游戏目录”创建第一个导入目标。"} />}

    <footer className="platform-directory-footer"><span>当前显示 {visibleRows.length} / {rows.length} 个目录</span><span>{reorderEnabled ? "当前可调整全局展示顺序" : "筛选状态下仅查看；清除筛选后可调整全局展示顺序"}</span></footer>

    {drawerOpen ? <><button className="platform-drawer-backdrop" type="button" aria-label="关闭新建游戏目录" disabled={busy === "create"} onClick={() => setDrawerOpen(false)} /><aside className="platform-drawer" role="dialog" aria-modal="true" aria-labelledby="platform-drawer-title"><form onSubmit={(event) => void create(event)}>
      <header><div><h2 id="platform-drawer-title">新建游戏目录</h2><p>仅在需要新增游戏平台或新的游戏集合时创建。</p></div><button ref={drawerCloseRef} className="platform-drawer-close" type="button" aria-label="关闭" disabled={busy === "create"} onClick={() => setDrawerOpen(false)}><AppIcon name="x" /></button></header>
      <div className="platform-drawer-body">
        <section className="platform-drawer-step"><span>1</span><div><h3>选择游戏平台</h3><label><span>游戏平台</span><select name="platformId" value={createPlatformID} onChange={(event) => selectCreatePlatform(event.target.value)}>{enabledPlatforms.map((platform) => <option value={platform.id} key={platform.id}>{platform.name}</option>)}</select></label></div></section>
        <section className="platform-drawer-step"><span>2</span><div><h3>定义目录信息</h3><label><span>目录名称</span><input name="name" value={createName} placeholder="例如：我的 GBA 游戏" required maxLength={200} onChange={(event) => setCreateName(event.target.value)} /></label><label><span>给用户看的说明</span><textarea name="description" value={createDescription} placeholder="说明这个目录收录了哪些游戏（可不填）" maxLength={10000} onChange={(event) => setCreateDescription(event.target.value)} /></label></div></section>
        <section className="platform-drawer-step"><span>3</span><div><h3>选择推荐运行方式</h3><label><span>推荐运行方式</span><select name="defaultCoreId" value={createCoreID} required onChange={(event) => setCreateCoreID(event.target.value)}>{selectedCreatePlatform?.cores.filter((core) => core.enabled).map((core) => <option value={core.id} key={core.id}>{core.name}</option>)}</select></label></div></section>
        <section className="platform-drawer-preview"><small>创建预览</small><strong>{selectedCreatePlatform?.name ?? "尚未选择平台"} · {createName.trim() || "未命名目录"}</strong><ul><li>默认使用 <b>{selectedCreateCore?.name ?? "尚未选择"}</b> 启动</li><li>创建后默认 <b>已启用</b></li><li>会追加到目录列表末尾，可随后拖动排序</li></ul></section>
      </div>
      <footer><button className="button secondary" type="button" disabled={busy === "create"} onClick={() => setDrawerOpen(false)}>取消</button><button className="button" disabled={busy !== null || !createPlatformID || !createCoreID}>{busy === "create" ? "正在创建…" : "创建目录"}</button></footer>
    </form></aside></> : null}

    <ConfirmDialog open={pending !== null} title={pending?.kind === "core" ? "确认更改推荐运行方式？" : "确认删除这个空目录？"} description={pending?.kind === "core" ? `“${pending.instance.name}”将改用 ${pending.coreName}。` : `“${pending?.instance.name ?? ""}”会从游戏目录中移除。`} confirmLabel={pending?.kind === "core" ? "应用更改" : "删除目录"} tone={pending?.kind === "delete" || (pending?.kind === "core" && pending.counts.blocked > 0) ? "danger" : "default"} busy={busy !== null} onCancel={() => setPending(null)} onConfirm={() => void confirmPending()}>
      {pending?.kind === "core" ? <ul><li>{pending.counts.ready} 款游戏可以继续运行</li><li>{pending.counts.needsValidation} 款游戏需要重新检查</li><li>{pending.counts.blocked > 0 ? `${pending.counts.blocked} 款游戏会暂时无法运行` : "没有游戏会被阻断"}</li><li>提交前会再次核对影响摘要，过期预览不会生效</li></ul> : <ul><li>只有没有游戏的目录可以删除</li><li>此操作不会删除基础平台或运行文件</li></ul>}
    </ConfirmDialog>

    <ConfirmDialog open={sortHelpOpen} title="目录排序说明" description="展示顺序决定用户侧目录筛选中的先后位置。" confirmLabel="知道了" hideCancel onCancel={() => setSortHelpOpen(false)} onConfirm={() => setSortHelpOpen(false)}><ul><li>清除搜索和筛选，并选择“展示顺序”后可以拖动排序</li><li>键盘聚焦拖动手柄后，可使用上下方向键移动</li><li>排序会一次性保存全部目录，失败时恢复原顺序</li></ul></ConfirmDialog>
  </div>;
}
