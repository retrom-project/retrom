"use client";

import type { DragEvent, FormEvent, KeyboardEvent } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast, type ToastMessage } from "@/components/flash-toast";
import { EmptyState, PageHeader } from "@/components/ui";
import type { EditTarget, PendingAction } from "./platform-manager";
import type { Platform, PlatformDirectoryFilters, PlatformInstance, PlatformRecommendations } from "./platform-directory-list";

type Summary = { total: number; enabled: number; disabled: number };

export type PlatformManagerViewProps = {
  busy: string | null;
  createCoreID: string;
  createDescription: string;
  createName: string;
  createPlatformID: string;
  draggedId: string | null;
  drawerOpen: boolean;
  editing: EditTarget;
  enabledPlatforms: Platform[];
  filters: PlatformDirectoryFilters;
  onApplyRecommendations: () => void;
  onConfirmPending: () => void;
  onCreate: (event: FormEvent<HTMLFormElement>) => void;
  onCreateCore: (value: string) => void;
  onCreateDescription: (value: string) => void;
  onCreateName: (value: string) => void;
  onDelete: (instance: PlatformInstance) => void;
  onDragEnd: () => void;
  onDrawer: (open: boolean) => void;
  onDrop: (event: DragEvent<HTMLDivElement>, targetId: string) => void;
  onEdit: (target: EditTarget) => void;
  onFilters: (patch: Partial<PlatformDirectoryFilters>) => void;
  onMenu: (id: string | null) => void;
  onMove: (id: string, index: number) => void;
  onPatch: (instance: PlatformInstance, body: Partial<Pick<PlatformInstance, "name" | "description" | "enabled">>) => void;
  onPendingClose: () => void;
  onPreviewCore: (instance: PlatformInstance, coreId: string) => void;
  onSelectCreatePlatform: (id: string) => void;
  onSortHelp: (open: boolean) => void;
  onStartDrag: (event: DragEvent<HTMLButtonElement>, id: string) => void;
  onSubmitInline: (event: FormEvent<HTMLFormElement>, instance: PlatformInstance, field: "name" | "description") => void;
  onToastDismiss: () => void;
  openMenuId: string | null;
  pending: PendingAction | null;
  platforms: Platform[];
  recommendationState: PlatformRecommendations | null;
  reorderEnabled: boolean;
  rows: PlatformInstance[];
  selectedCreateCore: { id: string; name: string } | undefined;
  selectedCreatePlatform: Platform | undefined;
  sortHelpOpen: boolean;
  summary: Summary;
  toast: ToastMessage | null;
  visibleRows: PlatformInstance[];
};

function RecommendationButton({ busy, onApply, recommendations }: { busy: string | null; onApply: () => void; recommendations: PlatformRecommendations | null }) {
  if (!recommendations) {return <button className="button secondary" type="button" disabled title="推荐目录暂时无法读取">推荐目录暂不可用</button>;}
  if (recommendations.summary.missingCount === 0) {
    const title = recommendations.summary.suppressedCount
      ? `推荐项均已处理；其中 ${recommendations.summary.suppressedCount} 个已停用或删除的目录不会自动恢复。`
      : "全部推荐目录均已覆盖";
    return <button className="button secondary" type="button" disabled title={title}>✓ 推荐目录已创建</button>;
  }
  return <button className="button secondary" type="button" disabled={busy !== null} aria-busy={busy === "recommendations"} title="只创建尚未覆盖的推荐游戏平台与运行方式组合，不修改已有目录。" onClick={onApply}>{busy === "recommendations" ? <><span className="button-spinner" aria-hidden="true" />正在创建…</> : `一键创建推荐目录 ${recommendations.summary.missingCount}`}</button>;
}

function DirectoryToolbar({ filters, onFilters, platforms, summary }: Pick<PlatformManagerViewProps, "filters" | "onFilters" | "platforms" | "summary">) {
  return <>
    <section className="platform-directory-toolbar" aria-label="筛选游戏目录">
      <label className="platform-directory-search"><span>搜索目录</span><span><AppIcon name="search" /><input type="search" value={filters.query} placeholder="输入目录名称、平台或说明" onChange={(event) => onFilters({ query: event.target.value })} /></span></label>
      <label><span>游戏平台</span><select value={filters.platformId} onChange={(event) => onFilters({ platformId: event.target.value })}><option value="">所有平台</option>{platforms.map((platform) => <option value={platform.id} key={platform.id}>{platform.name}</option>)}</select></label>
      <label><span>启用状态</span><select value={filters.status} onChange={(event) => onFilters({ status: event.target.value as PlatformDirectoryFilters["status"] })}><option value="ALL">全部状态</option><option value="ENABLED">已启用</option><option value="DISABLED">已停用</option></select></label>
      <label><span>排序方式</span><select value={filters.sort} onChange={(event) => onFilters({ sort: event.target.value as PlatformDirectoryFilters["sort"] })}><option value="ORDER">展示顺序</option><option value="NAME">名称 A-Z</option><option value="GAME_COUNT">游戏数</option></select></label>
    </section>
    <div className="platform-directory-quick"><div className="platform-directory-chips" aria-label="游戏目录快速筛选"><button type="button" className={filters.status === "ALL" ? "active" : ""} aria-pressed={filters.status === "ALL"} onClick={() => onFilters({ status: "ALL" })}>全部 {summary.total}</button><button type="button" className={filters.status === "ENABLED" ? "active" : ""} aria-pressed={filters.status === "ENABLED"} onClick={() => onFilters({ status: "ENABLED" })}>已启用 {summary.enabled}</button><button type="button" className={filters.status === "DISABLED" ? "active" : ""} aria-pressed={filters.status === "DISABLED"} onClick={() => onFilters({ status: "DISABLED" })}>已停用 {summary.disabled}</button></div><p><strong>拖动左侧手柄</strong>调整用户侧目录展示顺序</p></div>
  </>;
}

function InlineField({ busy, editing, field, instance, onEdit, onSubmit }: { busy: string | null; editing: EditTarget; field: "name" | "description"; instance: PlatformInstance; onEdit: (target: EditTarget) => void; onSubmit: PlatformManagerViewProps["onSubmitInline"] }) {
  const active = editing?.id === instance.id && editing.field === field;
  if (!active) {return field === "name" ? <h3>{instance.name}</h3> : <p>{instance.description || "暂无说明"}</p>;}
  return <form className="platform-directory-inline" onSubmit={(event) => onSubmit(event, instance, field)}>{field === "name" ? <input aria-label="游戏目录" name="name" defaultValue={instance.name} required maxLength={200} autoFocus /> : <textarea aria-label="给用户看的说明" name="description" defaultValue={instance.description} rows={1} maxLength={10000} autoFocus />}<button className="button" disabled={busy !== null}>保存</button><button className="icon-button" type="button" aria-label={field === "name" ? "取消修改目录名称" : "取消修改说明"} onClick={() => onEdit(null)}><AppIcon name="x" /></button></form>;
}

function DirectoryMenu({ busy, instance, menuOpen, onDelete, onEdit, onMenu, opensUp }: { busy: string | null; instance: PlatformInstance; menuOpen: boolean; onDelete: (instance: PlatformInstance) => void; onEdit: (target: EditTarget) => void; onMenu: (id: string | null) => void; opensUp: boolean }) {
  return <div className={`platform-more-wrap${opensUp ? " opens-up" : ""}`} role="cell"><button className="platform-directory-more" type="button" aria-label={`管理目录“${instance.name}”`} aria-expanded={menuOpen} onClick={() => onMenu(menuOpen ? null : instance.id)}>•••</button>{menuOpen ? <div className="platform-directory-menu" role="menu"><button type="button" role="menuitem" onClick={() => { onEdit({ id: instance.id, field: "name" }); onMenu(null); }}>编辑名称</button><button type="button" role="menuitem" onClick={() => { onEdit({ id: instance.id, field: "description" }); onMenu(null); }}>编辑说明</button><button className="danger" type="button" role="menuitem" disabled={instance.gameCount > 0 || busy !== null} onClick={() => { onDelete(instance); onMenu(null); }}>删除{instance.gameCount === 0 ? "空" : ""}目录</button>{instance.gameCount > 0 ? <small>还有 {instance.gameCount} 款游戏，无法删除</small> : null}</div> : null}</div>;
}

function DirectoryRow({ index, instance, props }: { index: number; instance: PlatformInstance; props: PlatformManagerViewProps }) {
  const globalIndex = props.rows.findIndex((row) => row.id === instance.id);
  const menuOpen = props.openMenuId === instance.id;
  const coreOptions = props.platforms.find((platform) => platform.id === instance.platformId)?.cores.filter((core) => core.enabled) ?? [];
  const className = `platform-directory-row${menuOpen ? " has-open-menu" : ""}${props.draggedId === instance.id ? " is-dragging" : ""}`;
  const keyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key === "ArrowUp") {event.preventDefault(); props.onMove(instance.id, globalIndex - 1);}
    if (event.key === "ArrowDown") {event.preventDefault(); props.onMove(instance.id, globalIndex + 1);}
  };
  return <div className={className} role="row" onDragOver={(event) => { if (props.reorderEnabled) {event.preventDefault();} }} onDrop={(event) => props.onDrop(event, instance.id)}>
    <div className="platform-directory-order" role="cell"><button className="platform-directory-handle" type="button" draggable={props.reorderEnabled && props.busy === null} disabled={!props.reorderEnabled || props.busy !== null} aria-label={`拖动“${instance.name}”调整顺序`} title={props.reorderEnabled ? "拖动排序；也可使用上下方向键" : "清除筛选并选择展示顺序后可排序"} onDragStart={(event) => props.onStartDrag(event, instance.id)} onDragEnd={props.onDragEnd} onKeyDown={keyDown}><AppIcon name="grip" /></button><span>{String(globalIndex + 1).padStart(2, "0")}</span></div>
    <div className="platform-directory-copy" role="cell"><InlineField busy={props.busy} editing={props.editing} field="name" instance={instance} onEdit={props.onEdit} onSubmit={props.onSubmitInline} /><InlineField busy={props.busy} editing={props.editing} field="description" instance={instance} onEdit={props.onEdit} onSubmit={props.onSubmitInline} /></div>
    <div className="platform-directory-platform" role="cell"><strong>{instance.platformName}</strong><small>平台实例</small></div>
    <div className="platform-directory-extensions" role="cell" aria-label={`${instance.platformName} 支持的扩展名`}>{instance.supportedExtensions.map((extension) => <code key={extension}>{extension}</code>)}</div>
    <div className="platform-directory-games" role="cell"><strong className={instance.gameCount === 0 ? "is-empty" : ""}>{instance.gameCount} 款</strong>{instance.gameCount === 0 ? <small>空目录</small> : null}</div>
    <div role="cell"><label className="sr-only" htmlFor={`core-${instance.id}`}>“{instance.name}”的推荐运行方式</label><select id={`core-${instance.id}`} value={instance.defaultCoreId} disabled={props.busy !== null} onChange={(event) => props.onPreviewCore(instance, event.target.value)}>{coreOptions.map((core) => <option value={core.id} key={core.id}>{core.name}</option>)}</select></div>
    <div className="platform-directory-state" role="cell"><label className={`platform-directory-toggle${instance.enabled ? "" : " off"}`} title={instance.enabled ? "取消勾选后，此目录中的游戏将从用户侧隐藏" : "勾选后，此目录中的游戏将重新显示在用户侧"}><input type="checkbox" aria-label={`“${instance.name}”启用状态`} checked={instance.enabled} disabled={props.busy !== null} onChange={(event) => props.onPatch(instance, { enabled: event.target.checked })} /><span className="box">{instance.enabled ? "✓" : "–"}</span><span>{instance.enabled ? "已启用" : "已停用"}</span></label></div>
    <DirectoryMenu busy={props.busy} instance={instance} menuOpen={menuOpen} onDelete={props.onDelete} onEdit={props.onEdit} onMenu={props.onMenu} opensUp={index >= props.visibleRows.length - 2} />
  </div>;
}

function DirectoryTable(props: PlatformManagerViewProps) {
  if (!props.visibleRows.length) {
    const hasRows = props.rows.length > 0;
    const actions = !hasRows ? <div className="platform-directory-empty-actions">{props.recommendationState && props.recommendationState.summary.missingCount > 0 ? <button className="button" type="button" disabled={props.busy !== null} onClick={props.onApplyRecommendations}>一键创建推荐目录 {props.recommendationState.summary.missingCount}</button> : null}<button className="button secondary" type="button" disabled={props.busy !== null} onClick={() => props.onDrawer(true)}>新建游戏目录</button></div> : undefined;
    return <EmptyState title={hasRows ? "没有匹配的游戏目录" : "还没有游戏目录"} description={hasRows ? "请调整搜索或筛选条件。" : "可以一次创建 Retrom 推荐目录，也可以只建立自己的主题目录。"} action={actions} />;
  }
  return <section className="platform-directory-table-scroll" aria-label="游戏目录表格，可横向滚动" tabIndex={0}><div className="platform-directory-table" role="table" aria-label="游戏目录"><div role="rowgroup"><div className="platform-directory-table-head" role="row"><span role="columnheader">顺序</span><span role="columnheader">游戏目录</span><span role="columnheader">游戏平台</span><span role="columnheader">扩展名</span><span role="columnheader">游戏数</span><span className="platform-directory-core-head" role="columnheader">推荐运行方式 <span className="platform-directory-info" tabIndex={0}>i<span role="tooltip">更改推荐运行方式时，系统会先检查此目录中现有游戏的兼容性，并在应用前展示影响结果。</span></span></span><span role="columnheader">启用状态</span><span role="columnheader">操作</span></div></div><div role="rowgroup">{props.visibleRows.map((instance, index) => <DirectoryRow index={index} instance={instance} props={props} key={instance.id} />)}</div></div></section>;
}

function CreateDrawer(props: PlatformManagerViewProps) {
  if (!props.drawerOpen) {return null;}
  return <><button className="platform-drawer-backdrop" type="button" aria-label="关闭新建游戏目录" disabled={props.busy === "create"} onClick={() => props.onDrawer(false)} /><aside className="platform-drawer" role="dialog" aria-modal="true" aria-labelledby="platform-drawer-title"><form onSubmit={props.onCreate}>
    <header><div><h2 id="platform-drawer-title">新建游戏目录</h2><p>仅在需要新增游戏平台或新的游戏集合时创建。</p></div><button id="platform-drawer-close" className="platform-drawer-close" type="button" aria-label="关闭" disabled={props.busy === "create"} onClick={() => props.onDrawer(false)}><AppIcon name="x" /></button></header>
    <div className="platform-drawer-body"><section className="platform-drawer-step"><span>1</span><div><h3>选择游戏平台</h3><label><span>游戏平台</span><select name="platformId" value={props.createPlatformID} onChange={(event) => props.onSelectCreatePlatform(event.target.value)}>{props.enabledPlatforms.map((platform) => <option value={platform.id} key={platform.id}>{platform.name}</option>)}</select></label></div></section><section className="platform-drawer-step"><span>2</span><div><h3>定义目录信息</h3><label><span>目录名称</span><input name="name" value={props.createName} placeholder="例如：我的 GBA 游戏" required maxLength={200} onChange={(event) => props.onCreateName(event.target.value)} /></label><label><span>给用户看的说明</span><textarea name="description" value={props.createDescription} placeholder="说明这个目录收录了哪些游戏（可不填）" maxLength={10000} onChange={(event) => props.onCreateDescription(event.target.value)} /></label></div></section><section className="platform-drawer-step"><span>3</span><div><h3>选择推荐运行方式</h3><label><span>推荐运行方式</span><select name="defaultCoreId" value={props.createCoreID} required onChange={(event) => props.onCreateCore(event.target.value)}>{props.selectedCreatePlatform?.cores.filter((core) => core.enabled).map((core) => <option value={core.id} key={core.id}>{core.name}</option>)}</select></label></div></section><section className="platform-drawer-preview"><small>创建预览</small><strong>{props.selectedCreatePlatform?.name ?? "尚未选择平台"} · {props.createName.trim() || "未命名目录"}</strong><ul><li>默认使用 <b>{props.selectedCreateCore?.name ?? "尚未选择"}</b> 启动</li><li>创建后默认 <b>已启用</b></li><li>会追加到目录列表末尾，可随后拖动排序</li></ul></section></div>
    <footer><button className="button secondary" type="button" disabled={props.busy === "create"} onClick={() => props.onDrawer(false)}>取消</button><button className="button" disabled={props.busy !== null || !props.createPlatformID || !props.createCoreID}>{props.busy === "create" ? "正在创建…" : "创建目录"}</button></footer>
  </form></aside></>;
}

function PendingDialog({ busy, onClose, onConfirm, pending }: { busy: string | null; onClose: () => void; onConfirm: () => void; pending: PendingAction | null }) {
  const core = pending?.kind === "core";
  const danger = pending?.kind === "delete" || Boolean(core && pending.counts.blocked > 0);
  return <ConfirmDialog open={pending !== null} title={core ? "确认更改推荐运行方式？" : "确认删除这个空目录？"} description={core ? `“${pending.instance.name}”将改用 ${pending.coreName}。` : `“${pending?.instance.name ?? ""}”会从游戏目录中移除。`} confirmLabel={core ? "应用更改" : "删除目录"} tone={danger ? "danger" : "default"} busy={busy !== null} onCancel={onClose} onConfirm={onConfirm}>{core ? <ul><li>{pending.counts.ready} 款游戏可以继续运行</li><li>{pending.counts.needsValidation} 款游戏需要重新检查</li><li>{pending.counts.blocked > 0 ? `${pending.counts.blocked} 款游戏会暂时无法运行` : "没有游戏会被阻断"}</li><li>提交前会再次核对影响摘要，过期预览不会生效</li></ul> : <ul><li>只有没有游戏的目录可以删除</li><li>此操作不会删除基础平台或运行文件</li></ul>}</ConfirmDialog>;
}

export function PlatformManagerView(props: PlatformManagerViewProps) {
  const announcement = props.busy === "recommendations" ? "正在创建推荐目录" : props.recommendationState?.summary.missingCount === 0 ? "推荐目录已创建" : "";
  return <div className="platform-directory-manager">
    <PageHeader eyebrow="管理后台" title="游戏目录" description="维护游戏集合及其推荐运行方式。一键创建只会补充缺失项，不会修改已有目录。" actions={<><button className="button secondary" type="button" disabled={props.busy !== null} onClick={() => props.onSortHelp(true)}>排序说明</button><RecommendationButton busy={props.busy} onApply={props.onApplyRecommendations} recommendations={props.recommendationState} /><button className="button" type="button" disabled={props.busy !== null} onClick={() => props.onDrawer(true)}><AppIcon name="plus" />新建游戏目录</button></>} />
    <Toast toast={props.toast} onDismiss={props.onToastDismiss} /><p className="sr-only" role="status" aria-live="polite">{announcement}</p>
    <DirectoryToolbar filters={props.filters} onFilters={props.onFilters} platforms={props.platforms} summary={props.summary} />
    <DirectoryTable {...props} />
    <footer className="platform-directory-footer"><span>当前显示 {props.visibleRows.length} / {props.rows.length} 个目录</span><span>{props.reorderEnabled ? "当前可调整全局展示顺序" : "筛选状态下仅查看；清除筛选后可调整全局展示顺序"}</span></footer>
    <CreateDrawer {...props} />
    <PendingDialog busy={props.busy} onClose={props.onPendingClose} onConfirm={props.onConfirmPending} pending={props.pending} />
    <ConfirmDialog open={props.sortHelpOpen} title="目录排序说明" description="展示顺序决定用户侧目录筛选中的先后位置。" confirmLabel="知道了" hideCancel onCancel={() => props.onSortHelp(false)} onConfirm={() => props.onSortHelp(false)}><ul><li>清除搜索和筛选，并选择“展示顺序”后可以拖动排序</li><li>键盘聚焦拖动手柄后，可使用上下方向键移动</li><li>排序会一次性保存全部目录，失败时恢复原顺序</li></ul></ConfirmDialog>
  </div>;
}
