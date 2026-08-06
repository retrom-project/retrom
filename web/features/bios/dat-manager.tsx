"use client";

import { useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { StatusBadge } from "@/components/ui";
import { writeHeaders } from "@/lib/api/client";
import { responseError, uploadOne, waitForJob } from "@/lib/upload";

export type DATVersion = {
  id: string;
  coreId: string;
  coreName: string;
  coreArtifactId: string;
  source: string;
  compatibilityStatus: string;
  parseStatus: string;
  active: boolean;
  machineCount: number | null;
  romEntryCount: number | null;
  diskEntryCount: number | null;
  biosSetCount: number | null;
  version: number;
  jobId?: string | null;
  jobState?: string | null;
  jobVersion?: number | null;
};

export type CoreArtifact = {
  id: string;
  coreId: string;
  coreName: string;
  emulatorjsVersion: string;
  bundleVersion: string;
  enabled: boolean;
  version: number;
};

type Diff = {
  baseDatVersionId: string | null;
  targetDatVersionId: string;
  summary: {
    schemaVersion: number;
    machines: ChangeCounts;
    romEntries: ChangeCounts;
    biosSets: ChangeCounts;
    dependencyTargets: ChangeCounts;
    warnings: number;
  };
  impact: { dependentPlatformInstanceCount?: number; variantRevalidationCount?: number };
  impactDigest: string;
  items: DiffItem[];
  nextCursor: string | null;
};

type ChangeCounts = { added: number; removed: number; changed: number };
type DiffItem = { section: string; change: "ADDED" | "REMOVED" | "CHANGED"; key: Record<string, string | number>; before: Record<string, unknown> | null; after: Record<string, unknown> | null };

const diffLabels: Array<[keyof Pick<Diff["summary"], "machines" | "romEntries" | "biosSets" | "dependencyTargets">, string]> = [
  ["machines", "Machines"], ["romEntries", "ROM entries"], ["biosSets", "BIOS sets"], ["dependencyTargets", "依赖目标"]
];

function stateLabel(item: DATVersion) {
  if (item.active) return "活动 · 可运行";
  return ({ PENDING: "待解析", PARSING: "解析中", READY: "可启用", FAILED: "失败", CANCELLED: "已取消" } as Record<string, string>)[item.parseStatus] ?? item.parseStatus;
}

export function DATManager({ versions, artifacts }: { versions: DATVersion[]; artifacts: CoreArtifact[] }) {
  const router = useRouter();
  const input = useRef<HTMLInputElement>(null);
  const [artifactId, setArtifactId] = useState(artifacts.find((item) => item.enabled && ["fbneo", "mame2003", "mame2003_plus"].includes(item.coreId))?.id ?? "");
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [diff, setDiff] = useState<Diff | null>(null);
  const [diffSection, setDiffSection] = useState("MACHINES");

  const artifactById = new Map(artifacts.map((artifact) => [artifact.id, artifact]));

  async function upload(file: File) {
    setBusy("upload"); setError(""); setNotice("");
    try {
      const uploaded = await uploadOne(file, setNotice);
      const response = await fetch("/api/v1/admin/arcade-dats", {
        method: "POST",
        credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() }),
        body: JSON.stringify({ uploadFileId: uploaded.uploadFileId, coreArtifactId: artifactId })
      });
      if (!response.ok) throw new Error(await responseError(response, "无法创建 DAT 候选"));
      const created = await response.json() as { datVersionId: string; jobId: string };
      setNotice(`候选 ${created.datVersionId} 已创建，正在由后端安全解析…`);
      await waitForJob(created.jobId, (state) => setNotice(`DAT 解析 · ${state}`));
      setNotice(`候选 ${created.datVersionId} 已解析，可查看差异后启用。`);
      router.refresh();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "DAT 上传失败");
      router.refresh();
    } finally {
      setBusy(null);
      if (input.current) input.current.value = "";
    }
  }

  async function preview(item: DATVersion) {
    setBusy(item.id); setError("");
    try {
      const response = await fetch(`/api/v1/admin/arcade-dats/${item.id}/diff?section=MACHINES&change=ALL&limit=50`, { cache: "no-store" });
      if (!response.ok) throw new Error(await responseError(response, "无法读取 DAT 差异"));
      setDiff(await response.json() as Diff);
      setDiffSection("MACHINES");
      setNotice(`已生成 ${item.coreName} 候选的当次影响摘要。`);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "DAT 差异读取失败");
    } finally { setBusy(null); }
  }

  async function loadDiffItems(section: string, append = false) {
    if (!diff) return;
    setBusy(diff.targetDatVersionId); setError("");
    try {
      const cursor = append && diff.nextCursor ? `&cursor=${encodeURIComponent(diff.nextCursor)}` : "";
      const response = await fetch(`/api/v1/admin/arcade-dats/${diff.targetDatVersionId}/diff?section=${section}&change=ALL&limit=50${cursor}`, { cache: "no-store" });
      if (!response.ok) throw new Error(await responseError(response, "无法读取 DAT 差异明细"));
      const next = await response.json() as Diff;
      setDiff({ ...next, items: append ? [...diff.items, ...next.items] : next.items });
      setDiffSection(section);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "DAT 差异明细读取失败");
    } finally { setBusy(null); }
  }

  async function change(item: DATVersion, rollback: boolean) {
    setBusy(item.id); setError("");
    try {
      let currentDiff = diff?.targetDatVersionId === item.id ? diff : null;
      if (!currentDiff) {
        const response = await fetch(`/api/v1/admin/arcade-dats/${item.id}/diff`, { cache: "no-store" });
        if (!response.ok) throw new Error(await responseError(response, "无法读取 DAT 差异"));
        currentDiff = await response.json() as Diff;
        setDiff(currentDiff);
      }
      const artifact = artifactById.get(item.coreArtifactId);
      if (!artifact) throw new Error("找不到目标 CoreArtifact 版本");
      const blocked = (currentDiff.impact.variantRevalidationCount ?? 0) > 0;
      const action = rollback ? "回滚" : "启用";
      if (!window.confirm(`${action} ${item.coreName} DAT？将为 ${currentDiff.impact.variantRevalidationCount ?? 0} 个运行版本建立重校验影响。`)) return;
      const response = await fetch(`/api/v1/admin/arcade-dats/${item.id}/${rollback ? "rollback" : "activate"}`, {
        method: "POST",
        credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${artifact.version}"`, "Idempotency-Key": crypto.randomUUID() }),
        body: JSON.stringify({ impactDigest: currentDiff.impactDigest, confirmBlocked: blocked, confirmUnknownCompatibility: item.compatibilityStatus === "UNKNOWN" })
      });
      if (!response.ok) throw new Error(await responseError(response, `DAT ${action}失败`));
      setNotice(`${item.coreName} DAT 已${action}；历史版本与锁定快照均保留。`);
      setDiff(null);
      router.refresh();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "DAT 状态变更失败");
    } finally { setBusy(null); }
  }

  async function cancel(item: DATVersion) {
    if (!item.jobId || !item.jobVersion) return;
    setBusy(item.id); setError("");
    try {
      const response = await fetch(`/api/v1/admin/jobs/${item.jobId}/cancel`, { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${item.jobVersion}"`, "Idempotency-Key": crypto.randomUUID() }), body: JSON.stringify({ reason: "用户从 DAT 管理页取消解析" }) });
      if (!response.ok) throw new Error(await responseError(response, "无法取消 DAT 解析"));
      setNotice("已请求取消；worker 会在下一个有界检查点确认。");
      router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "取消失败"); }
    finally { setBusy(null); }
  }

  async function remove(item: DATVersion) {
    if (!window.confirm(`删除未启用候选 ${item.id}？已被引用的版本不会被删除。`)) return;
    setBusy(item.id); setError("");
    try {
      const response = await fetch(`/api/v1/admin/arcade-dats/${item.id}`, { method: "DELETE", credentials: "same-origin", headers: await writeHeaders({ "If-Match": `"v${item.version}"` }) });
      if (!response.ok) throw new Error(await responseError(response, "DAT 候选不可删除"));
      setNotice("DAT 候选已删除；已引用或活动版本仍受保护。"); setDiff(null); router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "删除失败"); }
    finally { setBusy(null); }
  }

  return <div className="stack">
    <details className="panel" open><summary className="panel-head"><strong>上传候选 DAT</strong><span>原始 XML 只交给后端受限 parser</span></summary><div className="panel-body form-grid"><div className="field"><label htmlFor="dat-artifact">目标 CoreArtifact</label><select id="dat-artifact" value={artifactId} onChange={(event) => setArtifactId(event.target.value)}>{artifacts.filter((item) => item.enabled && ["fbneo", "mame2003", "mame2003_plus"].includes(item.coreId)).map((item) => <option value={item.id} key={item.id}>{item.coreName} · EJS {item.emulatorjsVersion} · {item.bundleVersion}</option>)}</select></div><div className="field"><label htmlFor="dat-file">DAT XML 文件</label><input ref={input} id="dat-file" type="file" accept=".dat,.xml,text/xml,application/xml" disabled={busy !== null || !artifactId} onChange={(event) => { const file = event.target.files?.[0]; if (file) void upload(file); }} /></div></div></details>
    {notice ? <p role="status" className="status good">{notice}</p> : null}
    {error ? <p role="alert" className="status bad">{error}</p> : null}
    {diff ? <section className="panel"><div className="panel-head"><div><h2>当次差异与影响预览</h2><p>{diff.baseDatVersionId ?? "无活动基线"} → {diff.targetDatVersionId}</p></div><button type="button" className="row-action" onClick={() => setDiff(null)}>关闭</button></div><div className="panel-body"><div className="metrics">{diffLabels.map(([name, label]) => { const counts = diff.summary[name]; return <div className="metric" key={name}><span>{label}</span><strong>+{counts.added.toLocaleString("zh-CN")} / −{counts.removed.toLocaleString("zh-CN")} / ~{counts.changed.toLocaleString("zh-CN")}</strong></div>; })}</div><p>影响 {diff.impact.dependentPlatformInstanceCount ?? 0} 个平台目录、{diff.impact.variantRevalidationCount ?? 0} 个运行版本；解析警告 {diff.summary.warnings.toLocaleString("zh-CN")} 项。提交时会重新计算摘要并拒绝过期预览。</p><div className="header-actions" aria-label="DAT 差异分区">{[["MACHINES", "Machines"], ["ROM_ENTRIES", "ROM entries"], ["BIOS_SETS", "BIOS sets"], ["DEPENDENCY_TARGETS", "依赖目标"]].map(([value, label]) => <button type="button" className={diffSection === value ? "button" : "button secondary"} disabled={busy !== null} onClick={() => void loadDiffItems(value)} key={value}>{label}</button>)}</div>{diff.items.length === 0 ? <p className="status good">当前分区没有差异。</p> : <div className="table-wrap"><table><thead><tr><th>变化</th><th>键</th><th>原值</th><th>新值</th></tr></thead><tbody>{diff.items.map((item) => <tr key={`${item.section}:${JSON.stringify(item.key)}`}><td><StatusBadge tone={item.change === "REMOVED" ? "bad" : item.change === "ADDED" ? "good" : "warn"}>{item.change}</StatusBadge></td><td><code>{JSON.stringify(item.key)}</code></td><td><small>{item.before ? JSON.stringify(item.before) : "—"}</small></td><td><small>{item.after ? JSON.stringify(item.after) : "—"}</small></td></tr>)}</tbody></table></div>}{diff.nextCursor ? <button type="button" className="button secondary" disabled={busy !== null} onClick={() => void loadDiffItems(diffSection, true)}>加载更多差异</button> : null}</div></section> : null}
    <section className="panel table-wrap"><table><thead><tr><th>核心 / Artifact</th><th>来源</th><th>状态</th><th>统计</th><th>兼容性</th><th>版本</th><th>操作</th></tr></thead><tbody>{versions.map((item) => <tr key={item.id}><td><strong>{item.coreName}</strong><small>{item.coreArtifactId.slice(0, 12)}…</small></td><td>{item.source}</td><td><StatusBadge tone={item.active ? "good" : item.parseStatus === "FAILED" ? "bad" : "neutral"}>{stateLabel(item)}</StatusBadge></td><td>{(item.machineCount ?? 0).toLocaleString("zh-CN")} machines<br /><small>{(item.romEntryCount ?? 0).toLocaleString("zh-CN")} ROM entries</small></td><td><StatusBadge tone={item.compatibilityStatus === "MATCHED" ? "good" : "warn"}>{item.compatibilityStatus}</StatusBadge></td><td>v{item.version}</td><td><div className="header-actions">{item.parseStatus === "READY" ? <button type="button" className="row-action" disabled={busy !== null} onClick={() => void preview(item)}>查看差异</button> : null}{!item.active && item.parseStatus === "READY" ? <button type="button" className="row-action" disabled={busy !== null} onClick={() => void change(item, item.source === "BUILTIN")}>{item.source === "BUILTIN" ? "回滚" : "启用"}</button> : null}{!item.active && ["PENDING", "PARSING"].includes(item.parseStatus) && item.jobId ? <button type="button" className="row-action" disabled={busy !== null} onClick={() => void cancel(item)}>取消</button> : null}{!item.active && item.source === "USER" && !["PENDING", "PARSING"].includes(item.parseStatus) ? <button type="button" className="row-action danger" disabled={busy !== null} onClick={() => void remove(item)}>删除</button> : null}</div></td></tr>)}</tbody></table></section>
  </div>;
}
