"use client";

import { useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { FeedbackBanner, StatusBadge } from "@/components/ui";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import { statusTone } from "@/lib/status";
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
type PendingDATAction = { kind: "change"; item: DATVersion; rollback: boolean; impact: Diff } | { kind: "delete"; item: DATVersion };

const diffLabels: Array<[keyof Pick<Diff["summary"], "machines" | "romEntries" | "biosSets" | "dependencyTargets">, string]> = [
  ["machines", "游戏条目"], ["romEntries", "ROM 文件"], ["biosSets", "BIOS 集合"], ["dependencyTargets", "依赖目标"]
];

function stateLabel(item: DATVersion) {
  if (item.active) return "活动 · 可运行";
  return ({ PENDING: "待解析", PARSING: "解析中", READY: "可启用", FAILED: "失败", CANCELLED: "已取消" } as Record<string, string>)[item.parseStatus] ?? item.parseStatus;
}

const changeLabels: Record<DiffItem["change"], string> = { ADDED: "新增", REMOVED: "移除", CHANGED: "变更" };

export function DATManager({ versions, artifacts }: { versions: DATVersion[]; artifacts: CoreArtifact[] }) {
  const router = useRouter();
  const input = useRef<HTMLInputElement>(null);
  const [artifactId, setArtifactId] = useState(artifacts.find((item) => item.enabled && ["fbneo", "mame2003", "mame2003_plus"].includes(item.coreId))?.id ?? "");
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [diff, setDiff] = useState<Diff | null>(null);
  const [diffSection, setDiffSection] = useState("MACHINES");
  const [pending, setPending] = useState<PendingDATAction | null>(null);

  const artifactById = new Map(artifacts.map((artifact) => [artifact.id, artifact]));

  async function upload(file: File) {
    setBusy("upload"); setError(""); setNotice("");
    try {
      const uploaded = await uploadOne(file, setNotice);
      const response = await fetch("/api/v1/admin/arcade-dats", {
        method: "POST",
        credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }),
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
      if (!artifactById.has(item.coreArtifactId)) throw new Error("找不到目标运行引擎版本");
      setPending({ kind: "change", item, rollback, impact: currentDiff });
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "DAT 状态变更失败");
    } finally { setBusy(null); }
  }

  async function confirmPending() {
    if (!pending) return;
    setBusy(pending.item.id); setError("");
    try {
      if (pending.kind === "delete") {
        const response = await fetch(`/api/v1/admin/arcade-dats/${pending.item.id}`, { method: "DELETE", credentials: "same-origin", headers: await writeHeaders({ "If-Match": `"v${pending.item.version}"` }) });
        if (!response.ok) throw new Error(await responseError(response, "DAT 候选不可删除"));
        setNotice("候选数据目录已删除；已引用或正在使用的版本仍受保护。"); setDiff(null); router.refresh();
        return;
      }
      const artifact = artifactById.get(pending.item.coreArtifactId);
      if (!artifact) throw new Error("找不到目标运行引擎版本");
      const action = pending.rollback ? "回滚" : "启用";
      const response = await fetch(`/api/v1/admin/arcade-dats/${pending.item.id}/${pending.rollback ? "rollback" : "activate"}`, {
        method: "POST",
        credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${artifact.version}"`, "Idempotency-Key": newUuid() }),
        body: JSON.stringify({ impactDigest: pending.impact.impactDigest, confirmBlocked: (pending.impact.impact.variantRevalidationCount ?? 0) > 0, confirmUnknownCompatibility: pending.item.compatibilityStatus === "UNKNOWN" })
      });
      if (!response.ok) throw new Error(await responseError(response, `DAT ${action}失败`));
      setNotice(`${pending.item.coreName} 数据目录已${action}；历史版本和现有游戏快照均会保留。`);
      setDiff(null);
      router.refresh();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "DAT 状态变更失败");
    } finally { setBusy(null); setPending(null); }
  }

  async function cancel(item: DATVersion) {
    if (!item.jobId || !item.jobVersion) return;
    setBusy(item.id); setError("");
    try {
      const response = await fetch(`/api/v1/admin/jobs/${item.jobId}/cancel`, { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${item.jobVersion}"`, "Idempotency-Key": newUuid() }), body: JSON.stringify({ reason: "用户从 DAT 管理页取消解析" }) });
      if (!response.ok) throw new Error(await responseError(response, "无法取消 DAT 解析"));
      setNotice("已请求取消；worker 会在下一个有界检查点确认。");
      router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "取消失败"); }
    finally { setBusy(null); }
  }

  return <div className="stack">
    <details className="panel" open><summary className="panel-head"><strong>上传街机数据目录</strong><span>上传后会先安全解析，再预览变化</span></summary><div className="panel-body form-grid"><div className="field"><label htmlFor="dat-artifact">目标核心版本</label><select id="dat-artifact" value={artifactId} onChange={(event) => setArtifactId(event.target.value)}>{artifacts.filter((item) => item.enabled && ["fbneo", "mame2003", "mame2003_plus"].includes(item.coreId)).map((item) => <option value={item.id} key={item.id}>{item.coreName} · {item.bundleVersion}</option>)}</select></div><div className="field"><span className="field-label">目录文件</span><input ref={input} id="dat-file" hidden type="file" accept=".dat,.xml,text/xml,application/xml" disabled={busy !== null || !artifactId} onChange={(event) => { const file = event.target.files?.[0]; if (file) void upload(file); }} /><button className="button secondary" type="button" disabled={busy !== null || !artifactId} onClick={() => input.current?.click()}>选择 DAT 或 XML 文件</button></div></div></details>
    {notice ? <FeedbackBanner tone="good">{notice}</FeedbackBanner> : null}
    {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
    {diff ? <section className="panel"><div className="panel-head"><div><h2>当次差异与影响预览</h2><p>比较当前目录与候选目录</p></div><button type="button" className="row-action" onClick={() => setDiff(null)}>关闭</button></div><div className="panel-body"><div className="metrics">{diffLabels.map(([name, label]) => { const counts = diff.summary[name]; return <div className="metric" key={name}><span>{label}</span><strong>+{counts.added.toLocaleString("zh-CN")} / −{counts.removed.toLocaleString("zh-CN")} / ~{counts.changed.toLocaleString("zh-CN")}</strong></div>; })}</div><p>影响 {diff.impact.dependentPlatformInstanceCount ?? 0} 个平台目录、{diff.impact.variantRevalidationCount ?? 0} 个运行版本；解析警告 {diff.summary.warnings.toLocaleString("zh-CN")} 项。提交时会重新计算摘要并拒绝过期预览。</p><div className="header-actions" aria-label="DAT 差异分区">{[["MACHINES", "游戏条目"], ["ROM_ENTRIES", "ROM 文件"], ["BIOS_SETS", "BIOS 集合"], ["DEPENDENCY_TARGETS", "依赖目标"]].map(([value, label]) => <button type="button" className={diffSection === value ? "button" : "button secondary"} disabled={busy !== null} onClick={() => void loadDiffItems(value)} key={value}>{label}</button>)}</div>{diff.items.length === 0 ? <p className="status good">当前分区没有差异。</p> : <div className="table-wrap"><table><thead><tr><th>变化</th><th>对象</th><th>原值</th><th>新值</th></tr></thead><tbody>{diff.items.map((item) => <tr key={`${item.section}:${JSON.stringify(item.key)}`}><td><StatusBadge tone={item.change === "REMOVED" ? "bad" : item.change === "ADDED" ? "good" : "warn"}>{changeLabels[item.change]}</StatusBadge></td><td><code>{JSON.stringify(item.key)}</code></td><td><small>{item.before ? JSON.stringify(item.before) : "—"}</small></td><td><small>{item.after ? JSON.stringify(item.after) : "—"}</small></td></tr>)}</tbody></table></div>}{diff.nextCursor ? <button type="button" className="button secondary" disabled={busy !== null} onClick={() => void loadDiffItems(diffSection, true)}>加载更多差异</button> : null}</div></section> : null}
    <section className="panel table-wrap"><table><thead><tr><th>运行方式</th><th>来源</th><th>处理状态</th><th>收录内容</th><th>匹配情况</th><th>操作</th></tr></thead><tbody>{versions.map((item) => <tr key={item.id}><td><strong>{item.coreName}</strong></td><td>{item.source === "BUILTIN" ? "系统内置" : "手动上传"}</td><td><StatusBadge tone={item.active ? "good" : statusTone(item.parseStatus)}>{stateLabel(item)}</StatusBadge></td><td>{(item.machineCount ?? 0).toLocaleString("zh-CN")} 个游戏<br /><small>{(item.romEntryCount ?? 0).toLocaleString("zh-CN")} 个文件条目</small></td><td><StatusBadge tone={statusTone(item.compatibilityStatus)}>{item.compatibilityStatus === "MATCHED" ? "已匹配" : "需要确认"}</StatusBadge></td><td><div className="header-actions">{item.parseStatus === "READY" ? <button type="button" className="row-action" disabled={busy !== null} onClick={() => void preview(item)}>查看变化</button> : null}{!item.active && item.parseStatus === "READY" ? <button type="button" className="row-action" disabled={busy !== null} onClick={() => void change(item, item.source === "BUILTIN")}>{item.source === "BUILTIN" ? "恢复此版本" : "启用"}</button> : null}{!item.active && ["PENDING", "PARSING"].includes(item.parseStatus) && item.jobId ? <button type="button" className="row-action" disabled={busy !== null} onClick={() => void cancel(item)}>取消</button> : null}{!item.active && item.source === "USER" && !["PENDING", "PARSING"].includes(item.parseStatus) ? <button type="button" className="row-action danger" disabled={busy !== null} onClick={() => setPending({ kind: "delete", item })}>删除</button> : null}</div></td></tr>)}</tbody></table></section>
    <ConfirmDialog open={pending !== null} title={pending?.kind === "delete" ? "删除这个候选数据目录？" : pending?.rollback ? "恢复这个数据目录版本？" : "启用这个数据目录？"} description={pending?.kind === "change" ? `将为 ${pending.item.coreName} 应用本次预览结果。` : "未启用的候选会从列表中移除。"} confirmLabel={pending?.kind === "delete" ? "删除候选" : pending?.rollback ? "确认恢复" : "确认启用"} tone={pending?.kind === "delete" || (pending?.kind === "change" && (pending.impact.impact.variantRevalidationCount ?? 0) > 0) ? "danger" : "default"} busy={busy !== null} onCancel={() => setPending(null)} onConfirm={() => void confirmPending()}><ul>{pending?.kind === "change" ? <><li>{pending.impact.impact.variantRevalidationCount ?? 0} 个游戏运行版本需要重新检查</li><li>{pending.impact.summary.warnings} 项解析警告</li><li>提交时会拒绝已经过期的预览</li></> : <><li>已经被游戏引用的版本不会删除</li><li>正在使用的版本仍受保护</li></>}</ul></ConfirmDialog>
  </div>;
}
