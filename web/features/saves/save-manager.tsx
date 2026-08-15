"use client";

import Image from "next/image";
import Link from "next/link";
import { FormEvent, useCallback, useEffect, useMemo, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { TagChips } from "@/components/tag-picker";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast, type ToastMessage } from "@/components/flash-toast";
import { EmptyState, PageHeader } from "@/components/ui";
import { LaunchButton } from "@/features/player/launch-button";
import { writeHeaders } from "@/lib/api/client";
import { responseError } from "@/lib/upload";
import {
  customSaveName,
  filterSaveItems,
  formatSaveDuration,
  formatSaveTime,
  groupSaveItems,
  latestAvailableSave,
  saveAvailable,
  saveLibraryStats,
  type SaveFilters,
  type SaveGroup,
  type SaveItem,
} from "./save-library";

export type { SaveItem } from "./save-library";

function availabilityMessage(save: SaveItem) {
  return save.availability.reasons.map((reason) =>
    reason.logicalName ? `${reason.logicalName} 当前不可用` : "运行依赖当前不可用").join("；") || "游戏或运行依赖当前不可用。";
}

function SaveCard({
  save,
  nowMs,
  busy,
  menuOpen,
  editing,
  onMenu,
  onEdit,
  onCancelEdit,
  onRename,
  onDelete,
}: {
  save: SaveItem;
  nowMs: number;
  busy: boolean;
  menuOpen: boolean;
  editing: boolean;
  onMenu: () => void;
  onEdit: () => void;
  onCancelEdit: () => void;
  onRename: (event: FormEvent<HTMLFormElement>) => void;
  onDelete: () => void;
}) {
  const available = saveAvailable(save);
  const customName = customSaveName(save.name);
  return <article className="save-library-card" data-save-menu={save.saveStateId}>
    <div className="save-library-shot">
      <Image src={save.screenshotUrl} alt={`${save.gameTitle} 存档画面`} fill sizes="(min-width: 1600px) 280px, 220px" unoptimized />
      {!available ? <span className="save-library-blocked">当前不可用</span> : null}
      <div className="save-library-resume">{available
        ? <LaunchButton gameId={save.gameId} saveStateId={save.saveStateId} returnTo="/saves" label="从这里继续" />
        : <button className="button" type="button" disabled>当前不可继续</button>}</div>
    </div>
    <div className="save-library-card-body">
      {editing ? <form className="save-library-editor" onSubmit={onRename}>
        <label className="sr-only" htmlFor={`save-name-${save.saveStateId}`}>存档名称</label>
        <input id={`save-name-${save.saveStateId}`} name="name" defaultValue={save.name} required maxLength={120} autoFocus />
        <button className="icon-button" aria-label="保存名称" title="保存名称" disabled={busy}><AppIcon name="check" /></button>
        <button className="icon-button" type="button" aria-label="取消修改" title="取消修改" disabled={busy} onClick={onCancelEdit}><AppIcon name="x" /></button>
      </form> : <div className="save-library-title-row">
        <time dateTime={new Date(save.createdAtMs).toISOString()}>{formatSaveTime(save.createdAtMs, nowMs)}</time>
        <button className="save-library-menu-button" type="button" aria-label={`存档“${save.name}”的更多操作`} aria-haspopup="menu" aria-expanded={menuOpen} disabled={busy} onClick={onMenu}>•••</button>
        {menuOpen ? <div className="save-library-menu" role="menu">
          <button type="button" role="menuitem" onClick={onEdit}><AppIcon name="pencil" />重命名</button>
          <button className="danger" type="button" role="menuitem" onClick={onDelete}><AppIcon name="x" />删除存档</button>
        </div> : null}
      </div>}
      <div className="save-library-card-meta"><span>当时已游玩 {formatSaveDuration(save.activeDurationMs)}</span>{save.discLabel ? <span className="save-disc-badge">{save.discLabel}</span> : null}<span>{formatSaveTime(save.createdAtMs, nowMs, false).split(" ")[0]}</span></div>
      {customName ? <p className="save-library-custom-name" title={customName}>{customName}</p> : null}
      {!available ? <p className="save-library-reason" role="alert">{availabilityMessage(save)}</p> : null}
    </div>
  </article>;
}

function SaveGameGroup({
  group,
  nowMs,
  expanded,
  busyId,
  menuId,
  editingId,
  onExpand,
  onMenu,
  onEdit,
  onCancelEdit,
  onRename,
  onDelete,
}: {
  group: SaveGroup;
  nowMs: number;
  expanded: boolean;
  busyId: string | null;
  menuId: string | null;
  editingId: string | null;
  onExpand: () => void;
  onMenu: (saveStateId: string) => void;
  onEdit: (saveStateId: string) => void;
  onCancelEdit: () => void;
  onRename: (event: FormEvent<HTMLFormElement>, save: SaveItem) => void;
  onDelete: (save: SaveItem) => void;
}) {
  return <section className={`save-library-group ${expanded ? "is-expanded" : ""} ${group.saves.length === 5 ? "has-five-saves" : ""}`}>
    <header className="save-library-group-head">
      <div className="save-library-group-main"><span className="save-library-group-icon"><AppIcon name="gamepad" /></span><div><h2>{group.gameTitle}</h2><TagChips tags={group.saves[0]?.tags ?? []} limit={2} label={`${group.gameTitle} 的标签`} /><p>{group.platform.name} · {group.coreNames.join(" / ")}</p></div></div>
      <div className="save-library-group-meta"><span><strong>{group.saves.length}</strong> 份存档</span><span>最近保存 <strong>{formatSaveTime(group.latestCreatedAtMs, nowMs, false)}</strong></span><Link href={`/games/${group.gameId}`}>查看游戏详情</Link></div>
    </header>
    <div className={`save-library-grid ${group.saves.length === 1 ? "is-single" : ""}`}>
      {group.saves.map((save) => <SaveCard
        save={save}
        nowMs={nowMs}
        busy={busyId !== null}
        menuOpen={menuId === save.saveStateId}
        editing={editingId === save.saveStateId}
        onMenu={() => onMenu(save.saveStateId)}
        onEdit={() => onEdit(save.saveStateId)}
        onCancelEdit={onCancelEdit}
        onRename={(event) => onRename(event, save)}
        onDelete={() => onDelete(save)}
        key={save.saveStateId}
      />)}
      {group.saves.length === 1 ? <div className="save-library-empty-slot">该游戏目前只有这一份存档</div> : null}
    </div>
    {group.saves.length > 4 ? <div className="save-library-group-foot"><button type="button" onClick={onExpand}>{expanded ? "收起存档 ↑" : `展开全部 ${group.saves.length} 份 ↓`}</button></div> : null}
  </section>;
}

export function SaveManager({ saves, nowMs, initialFilters }: { saves: SaveItem[]; nowMs: number; initialFilters?: Partial<SaveFilters> }) {
  const [items, setItems] = useState(saves);
  const [query, setQuery] = useState(initialFilters?.query ?? "");
  const [gameId, setGameId] = useState(initialFilters?.gameId ?? "");
  const [availability, setAvailability] = useState<SaveFilters["availability"]>(initialFilters?.availability ?? "AVAILABLE");
  const [sort, setSort] = useState<SaveFilters["sort"]>(initialFilters?.sort ?? "CREATED_DESC");
  const [expandedGames, setExpandedGames] = useState<Set<string>>(() => new Set());
  const [busyId, setBusyId] = useState<string | null>(null);
  const [menuId, setMenuId] = useState<string | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<SaveItem | null>(null);
  const [toast, setToast] = useState<ToastMessage | null>(null);
  const dismissToast = useCallback(() => setToast(null), []);

  useEffect(() => {
    if (!menuId) return;
    const closeOutside = (event: PointerEvent) => {
      const menu = event.target instanceof Element ? event.target.closest("[data-save-menu]") : null;
      if (menu?.getAttribute("data-save-menu") !== menuId) setMenuId(null);
    };
    const closeEscape = (event: KeyboardEvent) => { if (event.key === "Escape") setMenuId(null); };
    document.addEventListener("pointerdown", closeOutside);
    document.addEventListener("keydown", closeEscape);
    return () => {
      document.removeEventListener("pointerdown", closeOutside);
      document.removeEventListener("keydown", closeEscape);
    };
  }, [menuId]);
  useEffect(() => {
    const params = new URLSearchParams();
    if (query.trim()) params.set("q", query.trim());
    if (gameId) params.set("gameId", gameId);
    if (availability !== "AVAILABLE") params.set("availability", availability);
    if (sort !== "CREATED_DESC") params.set("sort", sort);
    const search = params.toString();
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${search ? `?${search}` : ""}`);
  }, [availability, gameId, query, sort]);

  const stats = useMemo(() => saveLibraryStats(items), [items]);
  const latest = useMemo(() => latestAvailableSave(items), [items]);
  const filtered = useMemo(() => filterSaveItems(items, { query, gameId, availability, sort }), [availability, gameId, items, query, sort]);
  const groups = useMemo(() => groupSaveItems(filtered), [filtered]);
  const games = useMemo(() => Array.from(new Map(items.map((save) => [save.gameId, { id: save.gameId, title: save.gameTitle }])).values())
    .sort((left, right) => left.title.localeCompare(right.title, "zh-CN") || left.id.localeCompare(right.id)), [items]);

  async function rename(event: FormEvent<HTMLFormElement>, save: SaveItem) {
    event.preventDefault(); setBusyId(save.saveStateId); setToast(null);
    const data = new FormData(event.currentTarget);
    const name = String(data.get("name") ?? "");
    try {
      const response = await fetch(`/api/v1/saves/${save.saveStateId}`, {
        method: "PATCH",
        credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${save.version}"` }),
        body: JSON.stringify({ name }),
      });
      if (!response.ok) throw new Error(await responseError(response, "存档重命名失败"));
      const updated = await response.json() as { name: string; version: number };
      setItems((current) => current.map((item) => item.saveStateId === save.saveStateId ? { ...item, name: updated.name, version: updated.version } : item));
      setEditingId(null);
      setToast({ tone: "good", message: "存档名称已更新。" });
    } catch (caught) {
      setToast({ tone: "bad", message: caught instanceof Error ? caught.message : "存档重命名失败" });
    } finally {
      setBusyId(null);
    }
  }

  async function remove(save: SaveItem) {
    setBusyId(save.saveStateId); setToast(null);
    try {
      const response = await fetch(`/api/v1/saves/${save.saveStateId}`, {
        method: "DELETE",
        credentials: "same-origin",
        headers: await writeHeaders({ "If-Match": `"v${save.version}"` }),
      });
      if (!response.ok) throw new Error(await responseError(response, "存档删除失败"));
      setItems((current) => current.filter((item) => item.saveStateId !== save.saveStateId));
      setToast({ tone: "good", message: "存档已删除，底层内容会继续按保留期保护。" });
    } catch (caught) {
      setToast({ tone: "bad", message: caught instanceof Error ? caught.message : "存档删除失败" });
    } finally {
      setBusyId(null); setPendingDelete(null);
    }
  }

  return <div className="page-layout page-layout-saves">
    <PageHeader eyebrow="我的游戏" title="我的存档" description="查看保存画面，找到想恢复的游戏状态，并随时从这里继续。" actions={<div className="save-head-summary"><div><span>存档</span><strong>{stats.saveCount} 份</strong></div><div><span>涉及游戏</span><strong>{stats.gameCount} 款</strong></div></div>} />

    {items.length > 0 ? <section className="save-latest-section" aria-labelledby="save-latest-heading">
      <div className="save-section-label"><div><h2 id="save-latest-heading">最近保存</h2><p>最近创建的一份可用手动存档</p></div></div>
      {latest ? <div className="save-latest-card">
        <div className="save-latest-shot"><Image src={latest.screenshotUrl} alt={`${latest.gameTitle} 最近存档画面`} fill sizes="360px" unoptimized /></div>
        <div className="save-latest-copy"><div className="save-latest-kicker"><i />最近保存</div><Link href={`/games/${latest.gameId}`}><h3>{latest.gameTitle}</h3></Link><p>{latest.platform.name} · {latest.core.name}{latest.discLabel ? ` · ${latest.discLabel}` : ""}</p><div className="save-latest-facts"><div><span>保存时间</span><strong>{formatSaveTime(latest.createdAtMs, nowMs)}</strong></div><div><span>当时已游玩</span><strong>{formatSaveDuration(latest.activeDurationMs)}</strong></div><div><span>{latest.discLabel ? "保存位置" : "存档状态"}</span><strong>{latest.discLabel ?? "可以继续"}</strong></div></div></div>
        <div className="save-latest-actions"><LaunchButton gameId={latest.gameId} saveStateId={latest.saveStateId} returnTo="/saves" label="从这里继续" /><Link className="button secondary" href={`/games/${latest.gameId}`}>查看游戏详情</Link><small>直接恢复这份手动存档</small></div>
      </div> : <div className="save-latest-unavailable">当前没有可以直接恢复的存档，请在下方查看异常原因。</div>}
    </section> : null}

    <section className="save-library-toolbar" aria-label="筛选存档">
      <label className="save-library-search"><span>搜索</span><span><AppIcon name="search" /><input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索游戏或存档名称" /></span></label>
      <label><span>游戏</span><select value={gameId} onChange={(event) => setGameId(event.target.value)}><option value="">所有游戏</option>{games.map((game) => <option value={game.id} key={game.id}>{game.title}</option>)}</select></label>
      <label><span>存档状态</span><select value={availability} onChange={(event) => setAvailability(event.target.value as SaveFilters["availability"])}><option value="AVAILABLE">可以继续</option><option value="ALL">全部存档</option><option value="BLOCKED">当前不可用</option></select></label>
      <label><span>排列</span><select value={sort} onChange={(event) => setSort(event.target.value as SaveFilters["sort"])}><option value="CREATED_DESC">最近保存优先</option><option value="CREATED_ASC">最早保存优先</option></select></label>
      <p>当前显示 <strong>{filtered.length}</strong> 份</p>
    </section>

    {groups.length > 0 ? <div className="save-library-groups">{groups.map((group) => <SaveGameGroup
      group={group}
      nowMs={nowMs}
      expanded={expandedGames.has(group.gameId)}
      busyId={busyId}
      menuId={menuId}
      editingId={editingId}
      onExpand={() => setExpandedGames((current) => { const next = new Set(current); if (next.has(group.gameId)) next.delete(group.gameId); else next.add(group.gameId); return next; })}
      onMenu={(saveStateId) => setMenuId((current) => current === saveStateId ? null : saveStateId)}
      onEdit={(saveStateId) => { setEditingId(saveStateId); setMenuId(null); }}
      onCancelEdit={() => setEditingId(null)}
      onRename={(event, save) => void rename(event, save)}
      onDelete={(save) => { setPendingDelete(save); setMenuId(null); }}
      key={group.gameId}
    />)}</div> : <EmptyState title={items.length === 0 ? "还没有手动存档" : "没有符合条件的存档"} description={items.length === 0 ? "游玩时使用工具栏的“创建存档”，存档会安全地出现在这里。" : "尝试更换游戏、存档状态或搜索关键词。"} />}

    <Toast toast={toast} onDismiss={dismissToast} />
    <ConfirmDialog open={pendingDelete !== null} title="删除这份存档？" description={`“${pendingDelete?.name ?? ""}”将从你的存档列表中移除。`} confirmLabel="删除存档" tone="danger" busy={busyId !== null} onCancel={() => setPendingDelete(null)} onConfirm={() => { if (pendingDelete) void remove(pendingDelete); }}><ul><li>删除后不能再从这份进度继续</li><li>底层内容会先进入引用保护期，不会立即清除</li></ul></ConfirmDialog>
  </div>;
}
