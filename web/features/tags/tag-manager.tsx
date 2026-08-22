"use client";

import Link from "next/link";
import { useRef, useState, type Dispatch, type RefObject, type SetStateAction } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import { EmptyState, FeedbackBanner, StatusBadge } from "@/components/ui";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import { responseError } from "@/lib/upload";

export type TagAdminItem = {
  tagId: string; name: string; status: "ACTIVE" | "DELETED"; version: number;
  usage: { publishedGameCount: number; deletedGameCount: number; reviewDraftCount: number; pegasusCollectionCount: number };
  createdAtMs: number; updatedAtMs: number; deletedAtMs: number | null;
};

export type TagAdminPage = {
  summary: { activeTagCount: number; taggedGameCount: number; pendingReviewCount: number };
  items: TagAdminItem[];
  nextCursor: string | null;
};

type CommonTagsResult = { createdItems: TagAdminItem[]; existingItems: TagAdminItem[] };

type Editor = { mode: "create" | "edit"; item: TagAdminItem | null };

export function TagManager({ initial, filters }: { initial: TagAdminPage; filters: { q: string; status: string; sort: string } }) {
  const [items, setItems] = useState(initial.items);
  const [summary, setSummary] = useState(initial.summary);
  const [nextCursor, setNextCursor] = useState(initial.nextCursor);
  const [editor, setEditor] = useState<Editor | null>(null);
  const [name, setName] = useState("");
  const [deleteItem, setDeleteItem] = useState<TagAdminItem | null>(null);
  const [confirmName, setConfirmName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const triggerRef = useRef<HTMLButtonElement>(null);
  const nameRef = useRef<HTMLInputElement>(null);

  function openEditor(next: Editor) {
    setEditor(next); setName(next.item?.name ?? ""); setError(""); setNotice("");
  }

  async function save() {
    if (!editor || !name.trim()) {return;}
    setBusy(true); setError("");
    try {
      const creating = editor.mode === "create";
      const response = await fetch(creating ? "/api/v1/admin/tags" : `/api/v1/admin/tags/${editor.item?.tagId}`, {
        method: creating ? "POST" : "PATCH",
        headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid(), ...(creating ? {} : { "If-Match": `"v${editor.item?.version}"` }) }),
        body: JSON.stringify({ name }), credentials: "same-origin",
      });
      if (!response.ok) {throw new Error(await responseError(response, creating ? "新建标签失败" : "更新标签失败"));}
      const saved = await response.json() as TagAdminItem;
      setItems((current) => creating ? [saved, ...current].sort((left, right) => left.name.localeCompare(right.name, "zh-CN")) : current.map((item) => item.tagId === saved.tagId ? saved : item));
      if (creating) {setSummary((current) => ({ ...current, activeTagCount: current.activeTagCount + 1 }));}
      setEditor(null);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "标签保存失败"); }
    finally { setBusy(false); }
  }

  async function remove() {
    if (!deleteItem) {return;}
    setBusy(true); setError("");
    try {
      const response = await fetch(`/api/v1/admin/tags/${deleteItem.tagId}`, {
        method: "DELETE", credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${deleteItem.version}"`, "Idempotency-Key": newUuid() }),
        body: JSON.stringify({ confirmName }),
      });
      if (!response.ok) {throw new Error(await responseError(response, "删除标签失败"));}
      setItems((current) => filters.status === "ACTIVE" ? current.filter((item) => item.tagId !== deleteItem.tagId) : current.map((item) => item.tagId === deleteItem.tagId ? { ...item, status: "DELETED", version: item.version + 1, deletedAtMs: Date.now(), updatedAtMs: Date.now() } : item));
      setSummary((current) => ({ ...current, activeTagCount: Math.max(0, current.activeTagCount - 1) }));
      setDeleteItem(null); setConfirmName("");
    } catch (caught) { setError(caught instanceof Error ? caught.message : "删除标签失败"); }
    finally { setBusy(false); }
  }

  async function loadMore() {
    if (!nextCursor) {return;}
    setBusy(true); setError("");
    try {
      const query = new URLSearchParams({ ...filters, limit: "100", cursor: nextCursor });
      const response = await fetch(`/api/v1/admin/tags?${query}`, { cache: "no-store" });
      if (!response.ok) {throw new Error(await responseError(response, "加载更多标签失败"));}
      const page = await response.json() as TagAdminPage;
      setItems((current) => mergeTagItems(current, page.items, filters.sort)); setNextCursor(page.nextCursor);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "加载更多标签失败"); }
    finally { setBusy(false); }
  }

  async function addCommonTags() {
    if (busy) {return;}
    setBusy(true); setError(""); setNotice("");
    try {
      const response = await fetch("/api/v1/admin/tags/defaults", {
        method: "POST", credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }),
        body: JSON.stringify({}),
      });
      if (!response.ok) {throw new Error(await responseError(response, "添加常用标签失败"));}
      const result = await response.json() as CommonTagsResult;
      const visibleCreated = filters.status === "DELETED" ? [] : result.createdItems.filter((item) => tagMatchesQuery(item, filters.q));
      setItems((current) => mergeTagItems(current, visibleCreated, filters.sort));
      setSummary((current) => ({ ...current, activeTagCount: current.activeTagCount + result.createdItems.length }));
      setNotice(result.createdItems.length
        ? `已添加 ${result.createdItems.length} 个常用标签，${result.existingItems.length} 个已存在。`
        : `${result.existingItems.length} 个常用标签已全部存在。`);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "添加常用标签失败"); }
    finally { setBusy(false); }
  }

  return <TagManagerView {...{
    addCommonTags, busy, confirmName, deleteItem, editor, error, filters, items, name, nameRef, nextCursor,
    notice, openEditor, remove, save, setConfirmName, setDeleteItem, setEditor, setError, setName, summary, triggerRef,
  }} onLoadMore={() => void loadMore()} />;
}

type TagManagerViewProps = {
  addCommonTags: () => Promise<void>;
  busy: boolean;
  confirmName: string;
  deleteItem: TagAdminItem | null;
  editor: Editor | null;
  error: string;
  filters: { q: string; status: string; sort: string };
  items: TagAdminItem[];
  name: string;
  nameRef: RefObject<HTMLInputElement | null>;
  nextCursor: string | null;
  notice: string;
  onLoadMore: () => void;
  openEditor: (editor: Editor) => void;
  remove: () => Promise<void>;
  save: () => Promise<void>;
  setConfirmName: Dispatch<SetStateAction<string>>;
  setDeleteItem: Dispatch<SetStateAction<TagAdminItem | null>>;
  setEditor: Dispatch<SetStateAction<Editor | null>>;
  setError: Dispatch<SetStateAction<string>>;
  setName: Dispatch<SetStateAction<string>>;
  summary: TagAdminPage["summary"];
  triggerRef: RefObject<HTMLButtonElement | null>;
};

function TagManagerView({
  addCommonTags, busy, confirmName, deleteItem, editor, error, filters, items, name, nameRef, nextCursor,
  notice, onLoadMore, openEditor, remove, save, setConfirmName, setDeleteItem, setEditor, setError, setName,
  summary, triggerRef,
}: TagManagerViewProps) {
  return <div className="tag-manager">
    <div className="tag-kpis" aria-label="标签摘要"><article><span>活动标签</span><strong>{summary.activeTagCount}</strong><small>实例上限 1000</small></article><article><span>已关联游戏</span><strong>{summary.taggedGameCount}</strong><small>至少包含一个活动标签</small></article><article><span>待审核引用</span><strong>{summary.pendingReviewCount}</strong><small>等待管理员发布或丢弃</small></article></div>
    <div className="tag-manager-toolbar"><form action="/admin/tags"><label><span>名称搜索</span><input name="q" defaultValue={filters.q} placeholder="搜索标签名称" /></label><label><span>状态</span><select name="status" defaultValue={filters.status}><option value="ACTIVE">活动标签</option><option value="DELETED">已删除</option><option value="ALL">全部状态</option></select></label><label><span>排序</span><select name="sort" defaultValue={filters.sort}><option value="NAME_ASC">名称 A–Z</option><option value="UPDATED_DESC">最近更新</option></select></label><button className="button secondary" type="submit">应用筛选</button><Link className="button ghost" href="/admin/tags">重置</Link></form><div className="tag-manager-actions"><button className="button secondary" type="button" disabled={busy} onClick={() => void addCommonTags()}>{busy ? "正在添加…" : "添加常用标签"}</button><button ref={triggerRef} className="button" type="button" disabled={busy} onClick={() => openEditor({ mode: "create", item: null })}>新建标签</button></div></div>
    {error && !editor && !deleteItem ? <FeedbackBanner tone="bad">{error}。请刷新后重试；版本冲突时页面会保留当前输入。</FeedbackBanner> : null}
    {notice && !editor && !deleteItem ? <FeedbackBanner tone="good">{notice}</FeedbackBanner> : null}
    <TagItems {...{ filters, items, openEditor, setConfirmName, setDeleteItem, setError }} />
    {nextCursor ? <button className="button secondary tag-load-more" type="button" disabled={busy} onClick={onLoadMore}>{busy ? "正在加载…" : "加载更多"}</button> : null}
    <TagEditorSheet {...{ busy, editor, error, name, nameRef, save, setEditor, setName, triggerRef }} />
    <TagDeleteDialog {...{ busy, confirmName, deleteItem, error, remove, setConfirmName, setDeleteItem }} />
  </div>;
}

function tagMatchesQuery(item: TagAdminItem, query: string) {
  const normalized = query.trim().replace(/\s+/gu, " ").toLocaleLowerCase();
  return !normalized || item.name.toLocaleLowerCase().includes(normalized);
}

function mergeTagItems(current: TagAdminItem[], incoming: TagAdminItem[], sort: string) {
  const merged = new Map(current.map((item) => [item.tagId, item]));
  for (const item of incoming) {merged.set(item.tagId, item);}
  return [...merged.values()].sort(sort === "UPDATED_DESC"
    ? (left, right) => right.updatedAtMs - left.updatedAtMs || right.tagId.localeCompare(left.tagId)
    : (left, right) => left.name.localeCompare(right.name, "zh-CN") || left.tagId.localeCompare(right.tagId));
}

function TagItems({ filters, items, openEditor, setConfirmName, setDeleteItem, setError }: Pick<
  TagManagerViewProps, "filters" | "items" | "openEditor" | "setConfirmName" | "setDeleteItem" | "setError"
>) {
  if (items.length === 0) {
    const title = filters.q ? "没有匹配的标签" : filters.status === "DELETED" ? "还没有已删除标签" : "还没有标签";
    const description = filters.q ? "请调整名称、状态或排序条件。" : "标签建立后才能用于游戏、普通导入和本地扫描。";
    const action = filters.status === "ACTIVE" && !filters.q
      ? <button className="button" type="button" onClick={() => openEditor({ mode: "create", item: null })}>新建第一个标签</button>
      : undefined;
    return <EmptyState {...{ action, description, title }} />;
  }
  const startDelete = (item: TagAdminItem) => {
    setDeleteItem(item);
    setConfirmName("");
    setError("");
  };
  return <div className="tag-table-wrap"><table className="tag-table"><thead><tr><th>名称</th><th>状态</th><th>已发布 / 已删除游戏</th><th>待审核</th><th>扫描映射</th><th>最近更新</th><th>操作</th></tr></thead><tbody>{items.map((item) => <TagRow key={item.tagId} item={item} onEdit={() => openEditor({ mode: "edit", item })} onDelete={() => startDelete(item)} />)}</tbody></table></div>;
}

function TagRow({ item, onDelete, onEdit }: { item: TagAdminItem; onDelete: () => void; onEdit: () => void }) {
  const active = item.status === "ACTIVE";
  return <tr><th scope="row"><strong title={item.name}>{item.name}</strong></th><td><StatusBadge tone={active ? "good" : "neutral"}>{active ? "活动" : "已删除"}</StatusBadge></td><td><Link href={`/admin/games?tagId=${encodeURIComponent(item.tagId)}&status=ALL`}>{item.usage.publishedGameCount} / {item.usage.deletedGameCount}</Link></td><td><Link href={`/admin/reviews?tagId=${encodeURIComponent(item.tagId)}`}>{item.usage.reviewDraftCount}</Link></td><td>{item.usage.pegasusCollectionCount}</td><td><time dateTime={new Date(item.updatedAtMs).toISOString()}>{new Date(item.updatedAtMs).toLocaleString("zh-CN")}</time></td><td><div className="tag-row-actions"><button type="button" disabled={!active} onClick={onEdit}>编辑</button><button type="button" className="danger-link" disabled={!active} onClick={onDelete}>删除</button></div></td></tr>;
}

function TagEditorSheet({ busy, editor, error, name, nameRef, save, setEditor, setName, triggerRef }: Pick<
  TagManagerViewProps, "busy" | "editor" | "error" | "name" | "nameRef" | "save" | "setEditor" | "setName" |
  "triggerRef"
>) {
  const close = () => setEditor(null);
  const validName = Boolean(name.trim()) && [...name].length <= 40;
  return <ResponsiveSheet open={Boolean(editor)} title={editor?.mode === "create" ? "新建标签" : "编辑标签"} description="名称会进行 Unicode 规范化、空白折叠和不区分大小写的唯一性检查。" placement="right" onClose={() => {if (!busy) {close();}}} returnFocusRef={triggerRef} initialFocusRef={nameRef} className="tag-editor-sheet" footer={<><button className="button secondary" type="button" disabled={busy} onClick={close}>取消</button><button className="button" type="button" disabled={busy || !validName} onClick={() => void save()}>{busy ? "正在保存…" : "保存标签"}</button></>}>
    <label className="tag-name-field"><span>标签名称</span><input ref={nameRef} aria-label="标签名称" maxLength={160} value={name} onChange={(event) => setName(event.target.value)} /><small>{[...name].length}/40 个字符</small></label><div className="tag-normalized-preview"><span>规范化预览</span><strong>{name.trim().replace(/\s+/gu, " ") || "—"}</strong></div>{error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}<p className="tag-editor-help">活动标签最多 1000 个；同名标签删除后可重新建立，但不会继承旧关系。</p>
  </ResponsiveSheet>;
}

function TagDeleteDialog({ busy, confirmName, deleteItem, error, remove, setConfirmName, setDeleteItem }: Pick<
  TagManagerViewProps, "busy" | "confirmName" | "deleteItem" | "error" | "remove" | "setConfirmName" |
  "setDeleteItem"
>) {
  const close = () => {if (!busy) {setDeleteItem(null);}};
  return <ConfirmDialog open={Boolean(deleteItem)} title="删除标签" description="删除后不会恢复；已有关系保留为历史证据，但立即从游戏、搜索和活动选择器中隐藏。" tone="danger" confirmLabel="删除标签" busy={busy} confirmDisabled={!deleteItem || confirmName !== deleteItem.name} onCancel={close} onConfirm={() => void remove()}>
    {deleteItem ? <div className="tag-delete-impact"><p>影响：{deleteItem.usage.publishedGameCount} 个已发布游戏、{deleteItem.usage.deletedGameCount} 个已删除游戏、{deleteItem.usage.reviewDraftCount} 个待审核草稿、{deleteItem.usage.pegasusCollectionCount} 个扫描映射。</p><label><span>输入完整名称“{deleteItem.name}”确认</span><input value={confirmName} onChange={(event) => setConfirmName(event.target.value)} /></label>{error ? <p role="alert">{error}</p> : null}</div> : null}
  </ConfirmDialog>;
}
