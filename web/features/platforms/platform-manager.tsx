"use client";

import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { StatusBadge } from "@/components/ui";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";

export type Platform = { id: string; name: string; enabled: boolean; cores: Array<{ id: string; name: string; enabled: boolean }> };
export type PlatformInstance = { id: string; platformId: string; platformName: string; defaultCoreId: string; defaultCoreName: string; name: string; slug: string; description: string; sortOrder: number; enabled: boolean; version: number };

async function message(response: Response) {
  const body = await response.json().catch(() => null) as { error?: { message?: string } } | null;
  return body?.error?.message ?? `请求失败（${response.status}）`;
}

export function PlatformManager({ instances, platforms, createOpen }: { instances: PlatformInstance[]; platforms: Platform[]; createOpen: boolean }) {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const firstPlatform = platforms.find((platform) => platform.enabled !== false) ?? platforms[0];
  const [createPlatformID, setCreatePlatformID] = useState(firstPlatform?.id ?? "");

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError(""); setNotice("");
    const values = new FormData(event.currentTarget);
    const body = { platformId: String(values.get("platformId")), defaultCoreId: String(values.get("defaultCoreId")), name: String(values.get("name")), slug: String(values.get("slug")), description: String(values.get("description")), sortOrder: Number(values.get("sortOrder")) };
    const response = await fetch("/api/v1/admin/platform-instances", { method: "POST", headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }), body: JSON.stringify(body) });
    if (!response.ok) setError(await message(response)); else { setNotice("平台目录已创建"); event.currentTarget.reset(); router.refresh(); }
    setBusy(false);
  }

  async function update(event: FormEvent<HTMLFormElement>, instance: PlatformInstance) {
    event.preventDefault(); setBusy(true); setError(""); setNotice("");
    const values = new FormData(event.currentTarget);
    const body = { name: String(values.get("name")), description: String(values.get("description")), sortOrder: Number(values.get("sortOrder")), enabled: values.get("enabled") === "on" };
    const response = await fetch(`/api/v1/admin/platform-instances/${instance.id}`, { method: "PATCH", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${instance.version}"` }), body: JSON.stringify(body) });
    if (!response.ok) setError(await message(response)); else { setNotice("目录设置已保存"); router.refresh(); }
    setBusy(false);
  }

  async function changeCore(instance: PlatformInstance, coreId: string) {
    if (coreId === instance.defaultCoreId) return;
    setBusy(true); setError(""); setNotice("");
    const headers = await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${instance.version}"` });
    const preview = await fetch(`/api/v1/admin/platform-instances/${instance.id}/default-core-preview`, { method: "POST", headers, body: JSON.stringify({ coreId, cursor: null, limit: 100 }) });
    if (!preview.ok) { setError(await message(preview)); setBusy(false); return; }
    const impact = await preview.json() as { impactDigest: string; counts: { blocked: number } };
    const coreName = platforms.flatMap((platform) => platform.cores).find((core) => core.id === coreId)?.name ?? coreId;
    const confirmed = window.confirm(impact.counts.blocked > 0
      ? `预览完成：改为 ${coreName} 后，${impact.counts.blocked} 个游戏将暂时无法运行。确认应用吗？`
      : `预览完成：改为 ${coreName} 不会阻断现有游戏。确认应用吗？`);
    if (!confirmed) { setBusy(false); return; }
    const confirmBlocked = impact.counts.blocked > 0;
    const commit = await fetch(`/api/v1/admin/platform-instances/${instance.id}/default-core`, { method: "POST", headers: { ...headers, "Idempotency-Key": newUuid() }, body: JSON.stringify({ coreId, impactDigest: impact.impactDigest, confirmBlocked }) });
    if (!commit.ok) setError(await message(commit)); else { setNotice("默认核心已更新"); router.refresh(); }
    setBusy(false);
  }

  async function remove(instance: PlatformInstance) {
    if (!window.confirm(`确认删除空目录“${instance.name}”？`)) return;
    setBusy(true); setError("");
    const response = await fetch(`/api/v1/admin/platform-instances/${instance.id}`, { method: "DELETE", headers: await writeHeaders({ "If-Match": `"v${instance.version}"` }) });
    if (!response.ok) setError(await message(response)); else { setNotice("目录已删除"); router.refresh(); }
    setBusy(false);
  }

  return <div className="stack"><details className="panel" open={createOpen}><summary className="panel-head"><strong>＋ 新建平台目录</strong><span>基础平台与网址标识创建后不可修改</span></summary><form className="panel-body form-grid" onSubmit={(event) => void create(event)}><div className="field"><label htmlFor="platform-create-base">基础平台</label><select id="platform-create-base" name="platformId" value={createPlatformID} onChange={(event) => setCreatePlatformID(event.target.value)}>{platforms.map((platform) => <option value={platform.id} key={platform.id}>{platform.name}</option>)}</select></div><div className="field"><label htmlFor="platform-create-core">默认核心</label><select id="platform-create-core" name="defaultCoreId" key={createPlatformID}>{platforms.find((platform) => platform.id === createPlatformID)?.cores.filter((core) => core.enabled).map((core) => <option value={core.id} key={core.id}>{core.name}</option>)}</select></div><div className="field"><label htmlFor="platform-create-name">目录名称</label><input id="platform-create-name" name="name" required maxLength={200} /></div><div className="field"><label htmlFor="platform-create-slug">网址标识</label><input id="platform-create-slug" name="slug" required pattern="[a-z0-9]+(?:-[a-z0-9]+)*" maxLength={80} /></div><div className="field full"><label htmlFor="platform-create-description">说明</label><textarea id="platform-create-description" name="description" /></div><div className="field"><label htmlFor="platform-create-sort">排序</label><input id="platform-create-sort" name="sortOrder" type="number" defaultValue={100} /></div><div className="field"><button className="button" disabled={busy}>创建目录</button></div></form></details>{notice ? <p role="status" className="status good">{notice}</p> : null}{error ? <p role="alert" className="status bad">{error}</p> : null}{instances.length ? <section className="panel table-wrap"><table><thead><tr><th>目录</th><th>基础平台</th><th>默认核心</th><th>状态</th><th>排序</th><th>操作</th></tr></thead><tbody>{instances.map((instance) => <tr key={instance.id}><td><strong>{instance.name}</strong><small>{instance.slug}</small></td><td>{instance.platformName}</td><td>{instance.defaultCoreName}</td><td><StatusBadge tone={instance.enabled ? "good" : "neutral"}>{instance.enabled ? "已启用" : "已停用"}</StatusBadge></td><td>{instance.sortOrder}</td><td><details><summary className="row-action">编辑</summary><form className="inline-editor" onSubmit={(event) => void update(event, instance)}><label>名称<input name="name" defaultValue={instance.name} /></label><label>说明<textarea name="description" defaultValue={instance.description} /></label><label>排序<input name="sortOrder" type="number" defaultValue={instance.sortOrder} /></label><label><input name="enabled" type="checkbox" defaultChecked={instance.enabled} /> 已启用</label><label>默认核心<select name="defaultCoreId" defaultValue={instance.defaultCoreId}>{platforms.find((platform) => platform.id === instance.platformId)?.cores.filter((core) => core.enabled).map((core) => <option value={core.id} key={core.id}>{core.name}</option>)}</select></label><button className="button secondary" type="button" disabled={busy} onClick={(event) => { const form = event.currentTarget.form; if (form) void changeCore(instance, String(new FormData(form).get("defaultCoreId") ?? "")); }}>预览并更改默认核心</button><div className="header-actions"><button className="button" disabled={busy}>保存目录设置</button><button className="button danger" type="button" disabled={busy} onClick={() => void remove(instance)}>删除空目录</button></div></form></details></td></tr>)}</tbody></table></section> : null}</div>;
}
