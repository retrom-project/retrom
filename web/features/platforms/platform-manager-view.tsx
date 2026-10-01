"use client";

import type { FormEvent } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast, type ToastMessage } from "@/components/flash-toast";
import { EmptyState, PageHeader } from "@/components/ui";
import type { EditTarget, PendingAction } from "./platform-manager";
import type { Platform, PlatformDirectoryFilters, PlatformInstance, PlatformRecommendations } from "./platform-directory-list";
import { DirectoryMenu } from "./directory-menu";
import { categoryForPlatform, categoryLabelForPlatform, directoryCategories, type DirectoryCategory } from "./platform-category";

export type PlatformManagerViewProps = {
  busy: string | null;
  createCoreID: string;
  createDescription: string;
  createName: string;
  createPlatformID: string;
  drawerOpen: boolean;
  editing: EditTarget;
  enabledPlatforms: Platform[];
  expandedCategories: DirectoryCategory[];
  onExpandedCategories: (categories: DirectoryCategory[]) => void;
  filters: PlatformDirectoryFilters;
  onApplyRecommendations: () => void;
  onConfirmPending: () => void;
  onCreate: (event: FormEvent<HTMLFormElement>) => void;
  onCreateCore: (value: string) => void;
  onCreateDescription: (value: string) => void;
  onCreateName: (value: string) => void;
  onDelete: (instance: PlatformInstance) => void;
  onDrawer: (open: boolean) => void;
  onEdit: (target: EditTarget) => void;
  onFilters: (patch: Partial<PlatformDirectoryFilters>) => void;
  onMenu: (id: string | null) => void;
  onPatch: (instance: PlatformInstance, body: Partial<Pick<PlatformInstance, "name" | "description" | "enabled">>) => void;
  onPendingClose: () => void;
  onPreviewCore: (instance: PlatformInstance, coreId: string) => void;
  onSelectCreatePlatform: (id: string) => void;
  onSubmitInline: (event: FormEvent<HTMLFormElement>, instance: PlatformInstance, field: "name" | "description") => void;
  onToastDismiss: () => void;
  openMenuId: string | null;
  pending: PendingAction | null;
  platforms: Platform[];
  recommendationState: PlatformRecommendations | null;
  rows: PlatformInstance[];
  selectedCreateCore: { id: string; name: string } | undefined;
  selectedCreatePlatform: Platform | undefined;
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

function DirectoryToolbar({ filters, onFilters, platforms }: Pick<PlatformManagerViewProps, "filters" | "onFilters" | "platforms">) {
  return <>
    <section className="platform-directory-toolbar" aria-label="筛选游戏目录">
      <label className="platform-directory-search"><span>搜索目录</span><span><AppIcon name="search" /><input type="search" value={filters.query} placeholder="输入目录名称、平台、说明或运行方式" onChange={(event) => onFilters({ query: event.target.value })} /></span></label>
      <label><span>游戏平台</span><select value={filters.platformId} onChange={(event) => onFilters({ platformId: event.target.value })}><option value="">所有平台</option>{platforms.map((platform) => <option value={platform.id} key={platform.id}>{platform.name}</option>)}</select></label>
      <label><span>启用状态</span><select value={filters.status} onChange={(event) => onFilters({ status: event.target.value as PlatformDirectoryFilters["status"] })}><option value="ALL">全部状态</option><option value="ENABLED">已启用</option><option value="DISABLED">已停用</option></select></label>
    </section>
  </>;
}

function InlineField({ busy, editing, field, instance, onEdit, onSubmit }: { busy: string | null; editing: EditTarget; field: "name" | "description"; instance: PlatformInstance; onEdit: (target: EditTarget) => void; onSubmit: PlatformManagerViewProps["onSubmitInline"] }) {
  const active = editing?.id === instance.id && editing.field === field;
  if (!active) {return field === "name" ? <h3>{instance.name}</h3> : <p>{instance.description || "暂无说明"}</p>;}
  return <form className="platform-directory-inline" onSubmit={(event) => onSubmit(event, instance, field)}>{field === "name" ? <input aria-label="游戏目录" name="name" defaultValue={instance.name} required maxLength={200} autoFocus /> : <textarea aria-label="给用户看的说明" name="description" defaultValue={instance.description} rows={1} maxLength={10000} autoFocus />}<button className="button" disabled={busy !== null}>保存</button><button className="icon-button" type="button" aria-label={field === "name" ? "取消修改目录名称" : "取消修改说明"} onClick={() => onEdit(null)}><AppIcon name="x" /></button></form>;
}

function DirectoryRow({ instance, props }: { instance: PlatformInstance; props: PlatformManagerViewProps }) {
  const menuOpen = props.openMenuId === instance.id;
  const coreOptions = props.platforms.find((platform) => platform.id === instance.platformId)?.cores.filter((core) => core.enabled) ?? [];
  const className = `platform-directory-row${menuOpen ? " has-open-menu" : ""}`;
  return <div className={className} role="row" id={`directory-${instance.id}`} tabIndex={-1}>
    <div className="platform-directory-copy" role="cell"><InlineField busy={props.busy} editing={props.editing} field="name" instance={instance} onEdit={props.onEdit} onSubmit={props.onSubmitInline} /><InlineField busy={props.busy} editing={props.editing} field="description" instance={instance} onEdit={props.onEdit} onSubmit={props.onSubmitInline} /></div>
    <div className="platform-directory-platform" role="cell"><strong>{instance.platformName}</strong><small>平台实例</small></div>
    <div className="platform-directory-extensions" role="cell" tabIndex={0} aria-label={`${instance.platformName} 支持的扩展名`}>{instance.supportedExtensions.map((extension) => <code key={extension}>{extension}</code>)}</div>
    <div className="platform-directory-games" role="cell"><strong className={instance.gameCount === 0 ? "is-empty" : ""}>{instance.gameCount} 款</strong>{instance.gameCount === 0 ? <small>空目录</small> : null}</div>
    <div role="cell"><label className="sr-only" htmlFor={`core-${instance.id}`}>“{instance.name}”的推荐运行方式</label><select id={`core-${instance.id}`} value={instance.defaultCoreId} disabled={props.busy !== null} onChange={(event) => props.onPreviewCore(instance, event.target.value)}>{coreOptions.map((core) => <option value={core.id} key={core.id}>{core.name}</option>)}</select></div>
    <div className="platform-directory-state" role="cell"><label className={`platform-directory-toggle${instance.enabled ? "" : " off"}`} title={instance.enabled ? "取消勾选后，此目录中的游戏将从用户侧隐藏" : "勾选后，此目录中的游戏将重新显示在用户侧"}><input type="checkbox" aria-label={`“${instance.name}”启用状态`} checked={instance.enabled} disabled={props.busy !== null} onChange={(event) => props.onPatch(instance, { enabled: event.target.checked })} /><span className="box">{instance.enabled ? "✓" : "–"}</span><span>{instance.enabled ? "已启用" : "已停用"}</span></label></div>
    <DirectoryMenu busy={props.busy} instance={instance} menuOpen={menuOpen} onDelete={props.onDelete} onEdit={props.onEdit} onMenu={props.onMenu} />
  </div>;
}

function DirectoryTable(props: PlatformManagerViewProps) {
  if (!props.visibleRows.length) {
    const hasRows = props.rows.length > 0;
    const actions = !hasRows ? <div className="platform-directory-empty-actions">{props.recommendationState && props.recommendationState.summary.missingCount > 0 ? <button className="button" type="button" disabled={props.busy !== null} onClick={props.onApplyRecommendations}>一键创建推荐目录 {props.recommendationState.summary.missingCount}</button> : null}<button className="button secondary" type="button" disabled={props.busy !== null} onClick={() => props.onDrawer(true)}>新建游戏目录</button></div> : undefined;
    return <EmptyState title={hasRows ? "没有匹配的游戏目录" : "还没有游戏目录"} description={hasRows ? "请调整搜索或筛选条件。" : "可以一次创建 Retrom 推荐目录，也可以只建立自己的主题目录。"} action={actions} />;
  }
  const filtered = Boolean(props.filters.query.trim() || props.filters.platformId || props.filters.status !== "ALL");
  const groups = directoryCategories.map((category) => ({
    ...category, rows: props.visibleRows.filter((row) => categoryForPlatform(row.platformId) === category.id),
  })).filter((category) => category.rows.length > 0);
  return <>
    <div className="platform-directory-group-actions">
      <span>{filtered ? "已展开匹配的分类" : "按平台类型浏览目录"}</span>
      <div><button className="button secondary" type="button" disabled={filtered} onClick={() => props.onExpandedCategories(groups.map((group) => group.id))}>全部展开</button><button className="button secondary" type="button" disabled={filtered} onClick={() => props.onExpandedCategories([])}>全部收起</button></div>
    </div>
    <div className="platform-directory-groups">
      {groups.map((group) => {
        const expanded = filtered || props.expandedCategories.includes(group.id);
        const toggle = () => props.onExpandedCategories(expanded ? props.expandedCategories.filter((id) => id !== group.id) : [...props.expandedCategories, group.id]);
        return <section className="platform-directory-group" key={group.id} aria-label={group.label}>
          <h2><button className="button secondary platform-directory-group-toggle" type="button" aria-label={`${group.label} ${group.rows.length} 个目录`} aria-expanded={expanded} aria-controls={`directory-group-${group.id}`} onClick={toggle} disabled={filtered}>
            <svg viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="m6 9 6 6 6-6" stroke="currentColor" strokeWidth="1.8" /></svg><span>{group.label}</span><small>{group.rows.length} 个目录</small>
          </button></h2>
          {expanded ? <div id={`directory-group-${group.id}`} className="platform-directory-table-scroll" tabIndex={0} role="region" aria-label={`${group.label}目录表格，可横向滚动`}>
            <div className="platform-directory-table" role="table" aria-label={`${group.label}游戏目录`}>
              <div role="rowgroup"><div className="platform-directory-table-head" role="row"><span role="columnheader">游戏目录</span><span role="columnheader">游戏平台</span><span role="columnheader">扩展名</span><span role="columnheader">游戏数</span><span role="columnheader">推荐运行方式</span><span role="columnheader">启用状态</span><span role="columnheader">操作</span></div></div>
              <div role="rowgroup">{group.rows.map((instance) => <DirectoryRow instance={instance} props={props} key={instance.id} />)}</div>
            </div>
          </div> : null}
        </section>;
      })}
    </div>
  </>;
}

function CreateDrawer(props: PlatformManagerViewProps) {
  if (!props.drawerOpen) {return null;}
  return <><button className="platform-drawer-backdrop" type="button" aria-label="关闭新建游戏目录" disabled={props.busy === "create"} onClick={() => props.onDrawer(false)} /><aside className="platform-drawer" role="dialog" aria-modal="true" aria-labelledby="platform-drawer-title"><form onSubmit={props.onCreate}>
    <header><div><h2 id="platform-drawer-title">新建游戏目录</h2><p>仅在需要新增游戏平台或新的游戏集合时创建。</p></div><button id="platform-drawer-close" className="platform-drawer-close" type="button" aria-label="关闭" disabled={props.busy === "create"} onClick={() => props.onDrawer(false)}><AppIcon name="x" /></button></header>
    <div className="platform-drawer-body"><section className="platform-drawer-step"><span>1</span><div><h3>选择游戏平台</h3><label><span>游戏平台</span><select name="platformId" value={props.createPlatformID} onChange={(event) => props.onSelectCreatePlatform(event.target.value)}>{props.enabledPlatforms.map((platform) => <option value={platform.id} key={platform.id}>{platform.name}</option>)}</select></label></div></section><section className="platform-drawer-step"><span>2</span><div><h3>定义目录信息</h3><label><span>目录名称</span><input name="name" value={props.createName} placeholder="例如：我的 GBA 游戏" required maxLength={200} onChange={(event) => props.onCreateName(event.target.value)} /></label><label><span>给用户看的说明</span><textarea name="description" value={props.createDescription} placeholder="说明这个目录收录了哪些游戏（可不填）" maxLength={10000} onChange={(event) => props.onCreateDescription(event.target.value)} /></label></div></section><section className="platform-drawer-step"><span>3</span><div><h3>选择推荐运行方式</h3><label><span>推荐运行方式</span><select name="defaultCoreId" value={props.createCoreID} required onChange={(event) => props.onCreateCore(event.target.value)}>{props.selectedCreatePlatform?.cores.filter((core) => core.enabled).map((core) => <option value={core.id} key={core.id}>{core.name}</option>)}</select></label></div></section><section className="platform-drawer-preview"><small>创建预览</small><strong>{props.selectedCreatePlatform?.name ?? "尚未选择平台"} · {props.createName.trim() || "未命名目录"}</strong><ul><li>默认使用 <b>{props.selectedCreateCore?.name ?? "尚未选择"}</b> 启动</li><li>所属分类：<b>{categoryLabelForPlatform(props.createPlatformID)}</b></li><li>创建后默认 <b>已启用</b></li><li>按创建时间排列在所属分类中</li></ul></section></div>
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
    <PageHeader title="游戏目录" description="维护游戏集合及其推荐运行方式。一键创建只会补充缺失项，不会修改已有目录。" actions={<><RecommendationButton busy={props.busy} onApply={props.onApplyRecommendations} recommendations={props.recommendationState} /><button className="button" type="button" disabled={props.busy !== null} onClick={() => props.onDrawer(true)}><AppIcon name="plus" />新建游戏目录</button></>} />
    <Toast toast={props.toast} onDismiss={props.onToastDismiss} /><p className="sr-only" role="status" aria-live="polite">{announcement}</p>
    <DirectoryToolbar filters={props.filters} onFilters={props.onFilters} platforms={props.platforms} />
    <DirectoryTable {...props} />
    <footer className="platform-directory-footer"><span>匹配 {props.visibleRows.length} / {props.rows.length} 个目录</span><span>分类内按创建时间从早到晚排列</span></footer>
    <CreateDrawer {...props} />
    <PendingDialog busy={props.busy} onClose={props.onPendingClose} onConfirm={props.onConfirmPending} pending={props.pending} />
  </div>;
}
