"use client";

import { useRouter } from "next/navigation";
import { useState, type DragEvent, type FormEvent } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { FeedbackBanner } from "@/components/ui";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";

export type Platform = { id: string; name: string; enabled: boolean; cores: Array<{ id: string; name: string; enabled: boolean }> };
export type PlatformInstance = { id: string; platformId: string; platformName: string; defaultCoreId: string; defaultCoreName: string; name: string; slug: string; description: string; sortOrder: number; enabled: boolean; version: number; gameCount: number };

type PendingAction =
  | { kind: "core"; instance: PlatformInstance; coreId: string; coreName: string; impactDigest: string; blocked: number }
  | { kind: "delete"; instance: PlatformInstance };

type EditTarget = { id: string; field: "name" | "description" } | null;

async function message(response: Response) {
  const body = await response.json().catch(() => null) as { error?: { message?: string } } | null;
  return body?.error?.message ?? `请求失败（${response.status}）`;
}

function slugify(value: string, platformId: string) {
  const slug = value.normalize("NFKD").replace(/[\u0300-\u036f]/g, "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 80);
  return slug || `${platformId || "game"}-library`;
}

export function PlatformManager({ instances, platforms, createOpen }: { instances: PlatformInstance[]; platforms: Platform[]; createOpen: boolean }) {
  const router = useRouter();
  const [rows, setRows] = useState(() => [...instances].sort((left, right) => left.sortOrder - right.sortOrder));
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState("");
  const firstPlatform = platforms.find((platform) => platform.enabled !== false) ?? platforms[0];
  const [createPlatformID, setCreatePlatformID] = useState(firstPlatform?.id ?? "");
  const [createName, setCreateName] = useState("");
  const [createSlug, setCreateSlug] = useState(() => slugify("", firstPlatform?.id ?? ""));
  const [slugEdited, setSlugEdited] = useState(false);
  const [pending, setPending] = useState<PendingAction | null>(null);
  const [editing, setEditing] = useState<EditTarget>(null);
  const [draggedId, setDraggedId] = useState<string | null>(null);

  function clearFeedback() { setError(""); }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy("create"); clearFeedback();
    const form = event.currentTarget;
    const values = new FormData(form);
    const sortOrder = (rows.at(-1)?.sortOrder ?? 0) + 100;
    const body = { platformId: String(values.get("platformId")), defaultCoreId: String(values.get("defaultCoreId")), name: String(values.get("name")), slug: String(values.get("slug")), description: String(values.get("description")), sortOrder };
    try {
      const response = await fetch("/api/v1/admin/platform-instances", { method: "POST", headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }), body: JSON.stringify(body) });
      if (!response.ok) throw new Error(await message(response));
      form.reset(); setCreateName(""); setCreateSlug(slugify("", createPlatformID)); setSlugEdited(false); router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "目录创建失败"); }
    finally { setBusy(null); }
  }

  async function patchInstance(instance: PlatformInstance, body: Partial<Pick<PlatformInstance, "name" | "description" | "enabled">>) {
    setBusy(instance.id); clearFeedback();
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
    setBusy(instance.id); clearFeedback();
    try {
      const headers = await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${instance.version}"` });
      const preview = await fetch(`/api/v1/admin/platform-instances/${instance.id}/default-core-preview`, { method: "POST", headers, body: JSON.stringify({ coreId, cursor: null, limit: 100 }) });
      if (!preview.ok) throw new Error(await message(preview));
      const impact = await preview.json() as { impactDigest: string; counts: { blocked: number } };
      const coreName = platforms.flatMap((platform) => platform.cores).find((core) => core.id === coreId)?.name ?? coreId;
      setPending({ kind: "core", instance, coreId, coreName, impactDigest: impact.impactDigest, blocked: impact.counts.blocked });
    } catch (caught) { setError(caught instanceof Error ? caught.message : "无法预览运行方式影响"); }
    finally { setBusy(null); }
  }

  async function confirmPending() {
    if (!pending) return;
    setBusy(pending.instance.id); clearFeedback();
    try {
      if (pending.kind === "core") {
        const headers = await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${pending.instance.version}"`, "Idempotency-Key": newUuid() });
        const response = await fetch(`/api/v1/admin/platform-instances/${pending.instance.id}/default-core`, { method: "POST", headers, body: JSON.stringify({ coreId: pending.coreId, impactDigest: pending.impactDigest, confirmBlocked: pending.blocked > 0 }) });
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
    setBusy("order"); clearFeedback();
    try {
      const response = await fetch("/api/v1/admin/platform-instances/order", { method: "PUT", headers: await writeHeaders({ "Content-Type": "application/json" }), body: JSON.stringify({ items: next.map((item) => ({ id: item.id, version: item.version })) }) });
      if (!response.ok) throw new Error(await message(response));
      const result = await response.json() as { items: Array<{ id: string; sortOrder: number; version: number }> };
      const projections = new Map(result.items.map((item) => [item.id, item]));
      setRows((current) => current.map((item) => ({ ...item, ...projections.get(item.id) })));
    } catch (caught) {
      setRows(previous); setError(caught instanceof Error ? caught.message : "目录排序失败");
    } finally { setBusy(null); }
  }

  function move(instanceId: string, targetIndex: number) {
    if (busy) return;
    const previous = [...rows];
    const sourceIndex = rows.findIndex((row) => row.id === instanceId);
    if (sourceIndex < 0) return;
    const bounded = Math.max(0, Math.min(rows.length - 1, targetIndex));
    if (sourceIndex === bounded) return;
    const next = [...rows];
    const [moved] = next.splice(sourceIndex, 1);
    next.splice(bounded, 0, moved);
    setRows(next); void persistOrder(next, previous);
  }

  function dropOn(event: DragEvent<HTMLTableRowElement>, targetId: string) {
    event.preventDefault();
    const sourceId = draggedId;
    setDraggedId(null);
    if (!sourceId || sourceId === targetId) return;
    move(sourceId, rows.findIndex((row) => row.id === targetId));
  }

  return <div className="stack">
    <details className="panel" open={createOpen}>
      <summary className="panel-head"><div><strong>新建游戏目录</strong><p>填写用户可见信息；创建后可直接拖动调整顺序。</p></div><span className="status info"><i />分步填写</span></summary>
      <form className="panel-body form-grid" onSubmit={(event) => void create(event)}>
        <div className="field"><label htmlFor="platform-create-base">游戏平台</label><select id="platform-create-base" name="platformId" value={createPlatformID} onChange={(event) => { const value = event.target.value; setCreatePlatformID(value); if (!slugEdited) setCreateSlug(slugify(createName, value)); }}>{platforms.map((platform) => <option value={platform.id} key={platform.id}>{platform.name}</option>)}</select></div>
        <div className="field"><label htmlFor="platform-create-name">目录名称</label><input id="platform-create-name" name="name" value={createName} onChange={(event) => { setCreateName(event.target.value); if (!slugEdited) setCreateSlug(slugify(event.target.value, createPlatformID)); }} placeholder="例如：我的 GBA 游戏" required maxLength={200} /></div>
        <div className="field"><label htmlFor="platform-create-core">推荐运行方式</label><select id="platform-create-core" name="defaultCoreId" key={createPlatformID}>{platforms.find((platform) => platform.id === createPlatformID)?.cores.filter((core) => core.enabled).map((core) => <option value={core.id} key={core.id}>{core.name}</option>)}</select></div>
        <div className="field full"><label htmlFor="platform-create-description">给用户看的说明</label><textarea id="platform-create-description" name="description" placeholder="说明这个目录收录了哪些游戏（可不填）" /></div>
        <details className="field full advanced-form-options"><summary>高级设置</summary><div className="form-grid"><div className="field"><label htmlFor="platform-create-slug">网址标识</label><input id="platform-create-slug" name="slug" value={createSlug} onChange={(event) => { setCreateSlug(event.target.value); setSlugEdited(true); }} required pattern="[a-z0-9]+(?:-[a-z0-9]+)*" maxLength={80} /><small>系统会自动生成；创建后不可修改。</small></div></div></details>
        <div className="field full"><button className="button" disabled={busy !== null}>{busy === "create" ? "正在创建…" : "创建目录"}</button></div>
      </form>
    </details>
    {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
    {rows.length ? <section className="panel table-wrap"><table className="platform-table"><thead><tr><th><span className="sr-only">排序</span></th><th>游戏目录</th><th>游戏平台</th><th>推荐运行方式</th><th>启用状态</th><th>删除</th></tr></thead><tbody>{rows.map((instance, index) => <tr key={instance.id} onDragOver={(event) => event.preventDefault()} onDrop={(event) => dropOn(event, instance.id)}>
      <td><button className="drag-handle" type="button" draggable disabled={busy !== null} aria-label={`拖动“${instance.name}”调整顺序`} title="拖动排序；也可使用上下方向键" onDragStart={() => setDraggedId(instance.id)} onDragEnd={() => setDraggedId(null)} onKeyDown={(event) => { if (event.key === "ArrowUp") { event.preventDefault(); move(instance.id, index - 1); } if (event.key === "ArrowDown") { event.preventDefault(); move(instance.id, index + 1); } }}><AppIcon name="grip" /></button></td>
      <td><div className="platform-directory-cell">{editing?.id === instance.id && editing.field === "name" ? <form className="platform-inline-form" onSubmit={(event) => void submitInline(event, instance, "name")}><input aria-label="游戏目录" name="name" defaultValue={instance.name} required maxLength={200} autoFocus /><button className="button compact" disabled={busy !== null}>保存</button><button className="icon-button" type="button" aria-label="取消修改目录名称" onClick={() => setEditing(null)}><AppIcon name="x" /></button></form> : <div className="platform-inline-value"><strong>{instance.name}</strong><button className="icon-button" type="button" aria-label={`修改“${instance.name}”的目录名称`} title="修改目录名称" onClick={() => setEditing({ id: instance.id, field: "name" })}><AppIcon name="pencil" /></button></div>}{editing?.id === instance.id && editing.field === "description" ? <form className="platform-inline-form description" onSubmit={(event) => void submitInline(event, instance, "description")}><textarea aria-label="给用户看的说明" name="description" defaultValue={instance.description} rows={1} maxLength={10000} autoFocus /><button className="button compact" disabled={busy !== null}>保存</button><button className="icon-button" type="button" aria-label="取消修改说明" onClick={() => setEditing(null)}><AppIcon name="x" /></button></form> : <div className="platform-inline-value description"><small>{instance.description || "暂无说明"}</small><button className="icon-button" type="button" aria-label={`修改“${instance.name}”给用户看的说明`} title="修改说明" onClick={() => setEditing({ id: instance.id, field: "description" })}><AppIcon name="pencil" /></button></div>}</div></td>
      <td>{instance.platformName}<small className="platform-game-count">{instance.gameCount} 个游戏</small></td>
      <td><label className="sr-only" htmlFor={`core-${instance.id}`}>“{instance.name}”的推荐运行方式</label><select id={`core-${instance.id}`} className="select platform-core-select" value={instance.defaultCoreId} disabled={busy !== null} onChange={(event) => void previewCore(instance, event.target.value)}>{platforms.find((platform) => platform.id === instance.platformId)?.cores.filter((core) => core.enabled).map((core) => <option value={core.id} key={core.id}>{core.name}</option>)}</select></td>
      <td><label className="platform-enabled" title={instance.enabled ? "取消勾选后，此目录中的游戏将从用户侧隐藏" : "勾选后，此目录中的游戏将重新显示在用户侧"}><input type="checkbox" aria-label={`“${instance.name}”启用状态`} checked={instance.enabled} disabled={busy !== null} onChange={(event) => void patchInstance(instance, { enabled: event.target.checked })} /><span>{instance.enabled ? "已启用" : "已停用"}</span></label></td>
      <td><button className="platform-delete" type="button" aria-label={`删除目录“${instance.name}”`} title={instance.gameCount > 0 ? `目录中还有 ${instance.gameCount} 个游戏，不能删除` : "删除空目录"} disabled={busy !== null || instance.gameCount > 0} onClick={() => setPending({ kind: "delete", instance })}><AppIcon name="x" /></button></td>
    </tr>)}</tbody></table></section> : null}
    <ConfirmDialog open={pending !== null} title={pending?.kind === "core" ? "确认更改推荐运行方式？" : "确认删除这个空目录？"} description={pending?.kind === "core" ? `“${pending.instance.name}”将改用 ${pending.coreName}。` : `“${pending?.instance.name ?? ""}”会从平台目录中移除。`} confirmLabel={pending?.kind === "core" ? "应用更改" : "删除目录"} tone={pending?.kind === "delete" || (pending?.kind === "core" && pending.blocked > 0) ? "danger" : "default"} busy={busy !== null} onCancel={() => setPending(null)} onConfirm={() => void confirmPending()}>
      {pending?.kind === "core" ? <ul><li>{pending.blocked > 0 ? `${pending.blocked} 个现有游戏会暂时无法运行` : "现有游戏不会被阻断"}</li><li>提交前会再次核对影响摘要，过期预览不会生效</li></ul> : <ul><li>只有没有游戏的目录可以删除</li><li>此操作不会删除基础平台或运行文件</li></ul>}
    </ConfirmDialog>
  </div>;
}
