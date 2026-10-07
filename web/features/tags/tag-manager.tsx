"use client";
import { useToast } from "@/components/toast-provider";
import Link from "next/link";
import { useCallback, useState } from "react";
import { api, result, ApiError } from "@/lib/api/client";
import { tagSaveError } from "./tag-save-error";
import type { Tag } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { PageHeader, EmptyState } from "@/components/ui";
import { ResourceState } from "@/components/resource-state";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ResponsiveSheet } from "@/components/responsive-sheet";
export function TagManager() {
  const [query, setQuery] = useState("");
  const [draft, setDraft] = useState("");
  const [offset, setOffset] = useState(0);
  const load = useCallback(async () => result(await api.GET("/api/v1/admin/tags", {
    params: { query: { offset, limit: 100, q: query } },
  })), [offset, query]);
  const tags = useResource(load);
  const [editing, setEditing] = useState<Tag | "new" | null>(null);
  const [deleting, setDeleting] = useState<Tag | null>(null);
  const [name, setName] = useState("");
  const [confirmName, setConfirmName] = useState("");
  const { notify } = useToast();
  const [busy, setBusy] = useState(false);
  function edit(tag: Tag | "new") {
    setName(tag === "new" ? "" : tag.name); setEditing(tag);
  }
  async function save() {
    setBusy(true);
    try {
      if (editing === "new") {
        result(await api.POST("/api/v1/admin/tags", { body: { name: name.trim() } }));
        setName(""); notify({ tone: "good", message: "标签已创建，可以继续添加。" });
      } else if (editing) {
        result(await api.PATCH("/api/v1/admin/tags/{tagId}", {
          params: { path: { tagId: editing.id } }, body: { name: name.trim(), version: editing.version },
        }));
        notify({ tone: "good", message: "标签已更新" });
        setEditing(null);
      }
      tags.reload();
    } catch (failure) { notify({ tone: "bad", message: tagSaveError(failure) }); }
    finally { setBusy(false); }
  }
  async function remove() {
    if (!deleting) { return; }
    setBusy(true);
    try {
      const response = await api.DELETE("/api/v1/admin/tags/{tagId}", {
        params: { path: { tagId: deleting.id } }, body: { version: deleting.version },
      });
      if (response.error) { throw new ApiError(response.error.code, response.error.message, response.response.status); }
      notify({ tone: "good", message: "标签已删除" });
      setDeleting(null); tags.reload();
    } catch (failure) { notify({ tone: "bad", message: failure instanceof Error ? failure.message : "删除失败，请重试。" }); }
    finally { setBusy(false); }
  }
  return <>
    <PageHeader title="标签管理" description="建立标签用于游戏分类与筛选，标签由整个资料库共享。" />
    <div className="tag-manager">
      <div className="tag-kpis" aria-label="标签摘要"><article><span>{query ? "匹配标签" : "活动标签"}</span><strong>{tags.data?.total ?? "—"}</strong><small>用于游戏分类与筛选</small></article></div>
      <div className="tag-manager-toolbar">
        <form onSubmit={(event) => { event.preventDefault(); setQuery(draft); setOffset(0); }}>
          <label><span>名称搜索</span><input value={draft} placeholder="搜索标签名称" onChange={(event) => setDraft(event.target.value)} /></label>
          <button className="button" type="submit">应用筛选</button>
          <button className="button secondary" type="button" onClick={() => { setDraft(""); setQuery(""); setOffset(0); }}>重置</button>
        </form>
        <div className="tag-manager-actions"><button className="button" onClick={() => edit("new")}>新建标签</button></div>
      </div>
      <ResourceState resource={tags}>{(data) => <>
        {data.items.length ? <TagTable items={data.items} onEdit={edit} onDelete={(tag) => { setConfirmName(""); setDeleting(tag); }} /> : <EmptyState title={query ? "没有匹配的标签" : "还没有标签"} description="标签建立后可以用于游戏分类与筛选。" action={<button className="button" onClick={() => edit("new")}>新建第一个标签</button>} />}
        {data.total > 100 ? <div className="list-pagination"><span>当前展示 {data.items.length} / {data.total} 个标签</span><div><button className="button secondary" disabled={!offset} onClick={() => setOffset(Math.max(0, offset - 100))}>上一页</button><button className="button secondary" disabled={offset + 100 >= data.total} onClick={() => setOffset(offset + 100)}>下一页</button></div></div> : null}
      </>}</ResourceState>
    </div>
    <TagEditor editing={editing} busy={busy} name={name} onName={setName} onClose={() => setEditing(null)} onSave={() => void save()} />
    <ConfirmDialog open={!!deleting} title="删除标签" description="删除后不再显示此标签。同名重新创建的标签需要重新关联游戏。" tone="danger" busy={busy} confirmLabel="删除标签" confirmDisabled={confirmName !== deleting?.name} onCancel={() => setDeleting(null)} onConfirm={() => void remove()}>
      {deleting ? <div className="tag-delete-impact"><p>关联 {deleting.gameCount} 款已发布游戏。</p><label><span>输入完整名称“{deleting.name}”确认</span><input value={confirmName} onChange={(event) => setConfirmName(event.target.value)} /></label></div> : null}
    </ConfirmDialog>
  </>;
}
function TagTable({ items, onEdit, onDelete }: { items: Tag[]; onEdit: (tag: Tag) => void; onDelete: (tag: Tag) => void }) {
  return <div className="tag-table-wrap"><table className="tag-table"><thead><tr><th>名称</th><th>状态</th><th>已发布游戏</th><th>操作</th></tr></thead><tbody>
    {items.map((tag) => <tr key={tag.id}><th scope="row"><strong title={tag.name}>{tag.name}</strong></th><td><span className="status good">活动</span></td><td><Link href={`/admin/games?tagId=${tag.id}`}>{tag.gameCount} 款</Link></td><td><div className="tag-row-actions"><button className="button secondary" onClick={() => onEdit(tag)}>编辑</button><button className="button secondary tag-delete-button" onClick={() => onDelete(tag)}>删除</button></div></td></tr>)}
  </tbody></table></div>;
}
function TagEditor({ editing, busy, name, onName, onClose, onSave }: {
  editing: Tag | "new" | null; busy: boolean; name: string;
  onName: (value: string) => void; onClose: () => void; onSave: () => void;
}) {
  return <ResponsiveSheet open={editing !== null} busy={busy} title={editing === "new" ? "新建标签" : "编辑标签"} description="使用简短、清晰的名称整理游戏。" placement="right" className="tag-editor-sheet" onClose={onClose} footer={<div className="tag-editor-footer-content"><div className="tag-editor-actions"><button className="button secondary" disabled={busy} onClick={onClose}>取消</button><button className="button" disabled={busy || !name.trim() || [...name].length > 40} onClick={onSave}>{busy ? "正在保存…" : "保存标签"}</button></div></div>}>
    <label className="tag-name-field"><span>标签名称</span><input aria-label="标签名称" maxLength={160} value={name} onChange={(event) => onName(event.target.value)} /><small>{[...name].length}/40 个字符</small></label>
    <div className="tag-normalized-preview"><span>保存名称</span><strong>{name.trim() || "—"}</strong></div>
    <p className="tag-editor-help">同名标签删除后可以重新创建，但不会恢复原来的游戏关联。</p>
  </ResponsiveSheet>;
}
