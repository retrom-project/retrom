"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ButtonLink, PageHeader, StatusBadge } from "@/components/ui";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import { statusTone } from "@/lib/status";
import { responseError, uploadOne, waitForJob } from "@/lib/upload";
import { filterDATVersions, summarizeDAT, type CoreArtifact, type DATFilters, type DATQuickFilter, type DATVersion } from "./runtime-dependencies";

export type { CoreArtifact, DATVersion } from "./runtime-dependencies";

type ChangeCounts = { added: number; removed: number; changed: number };
type DiffItem = { section: string; change: "ADDED" | "REMOVED" | "CHANGED"; key: Record<string, string | number>; before: Record<string, unknown> | null; after: Record<string, unknown> | null };
type Diff = {
  baseDatVersionId: string | null;
  targetDatVersionId: string;
  summary: { schemaVersion: number; machines: ChangeCounts; romEntries: ChangeCounts; biosSets: ChangeCounts; dependencyTargets: ChangeCounts; warnings: number };
  impact: { dependentPlatformInstanceCount?: number; variantRevalidationCount?: number };
  impactDigest: string;
  items: DiffItem[];
  nextCursor: string | null;
};
type DiffIntent = { item: DATVersion; rollback: boolean } | null;

const arcadeCores = new Set(["fbneo", "mame2003", "mame2003_plus"]);
const diffLabels: Array<[keyof Pick<Diff["summary"], "machines" | "romEntries" | "biosSets" | "dependencyTargets">, string]> = [
  ["machines", "游戏条目"], ["romEntries", "ROM 文件"], ["biosSets", "BIOS 集合"], ["dependencyTargets", "依赖目标"],
];
const diffSections = [["MACHINES", "游戏条目"], ["ROM_ENTRIES", "ROM 文件"], ["BIOS_SETS", "BIOS 集合"], ["DEPENDENCY_TARGETS", "依赖目标"]] as const;
const changeLabels: Record<DiffItem["change"], string> = { ADDED: "新增", REMOVED: "移除", CHANGED: "变更" };

function sourceLabel(source: string) { return source === "BUILTIN" ? "系统内置" : "手动上传"; }

function stateLabel(item: DATVersion) {
  if (item.active) return "当前启用";
  if (item.source === "BUILTIN" && item.parseStatus === "READY") return "历史版本";
  return ({ PENDING: "等待解析", PARSING: "正在解析", READY: "可以启用", FAILED: "解析失败", CANCELLED: "已取消" } as Record<string, string>)[item.parseStatus] ?? item.parseStatus;
}

function compatibilityLabel(status: string) {
  return ({ MATCHED: "已匹配", UNKNOWN: "需要确认", INCOMPATIBLE: "不兼容" } as Record<string, string>)[status] ?? status;
}

function formatDate(value: number) {
  return new Intl.DateTimeFormat("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" }).format(new Date(value));
}

function formatRecord(value: Record<string, unknown> | null) {
  if (!value) return "—";
  return Object.entries(value).map(([key, item]) => `${key}: ${typeof item === "object" ? JSON.stringify(item) : String(item)}`).join(" · ") || "—";
}

function DATActiveCard({ item, artifact }: { item: DATVersion; artifact?: CoreArtifact }) {
  return <article className="runtime-active-card panel">
    <div className="runtime-active-top"><div><StatusBadge tone="good">当前启用</StatusBadge><h3>{item.coreName}</h3><p>{sourceLabel(item.source)}{artifact ? ` · EmulatorJS ${artifact.emulatorjsVersion}` : ""}</p></div><span className="runtime-active-dot" title="当前实际生效" aria-label="当前实际生效" /></div>
    <dl className="runtime-active-metrics"><div><dt>游戏条目</dt><dd>{item.machineCount?.toLocaleString("zh-CN") ?? "—"}</dd></div><div><dt>BIOS 集合</dt><dd>{item.biosSetCount?.toLocaleString("zh-CN") ?? "—"}</dd></div><div><dt>匹配状态</dt><dd>{compatibilityLabel(item.compatibilityStatus)}</dd></div></dl>
    <footer><span>最后启用：{formatDate(item.updatedAtMs)}</span><span>bundle {artifact?.bundleVersion ?? "—"}</span></footer>
  </article>;
}

function DATDiffContent({ diff, versions, busy, section, onSection, onMore }: {
  diff: Diff;
  versions: DATVersion[];
  busy: boolean;
  section: string;
  onSection: (section: string) => void;
  onMore: () => void;
}) {
  const base = versions.find((item) => item.id === diff.baseDatVersionId);
  const target = versions.find((item) => item.id === diff.targetDatVersionId);
  return <div className="runtime-diff">
    <div className="runtime-diff-compare">
      <article><StatusBadge tone="good">当前启用</StatusBadge><h3>{base?.coreName ?? target?.coreName ?? "当前目录"}</h3><p>{base ? `${sourceLabel(base.source)} · ${(base.machineCount ?? 0).toLocaleString("zh-CN")} 个游戏` : "当前没有可比较的启用版本"}</p></article>
      <span aria-hidden="true">→</span>
      <article><StatusBadge tone="info">目标版本</StatusBadge><h3>{target?.coreName ?? "候选目录"}</h3><p>{target ? `${sourceLabel(target.source)} · ${(target.machineCount ?? 0).toLocaleString("zh-CN")} 个游戏` : "等待读取版本信息"}</p></article>
    </div>
    <div className="runtime-diff-metrics">{diffLabels.map(([name, label]) => { const counts = diff.summary[name]; return <article key={name}><small>{label}</small><strong>+{counts.added.toLocaleString("zh-CN")} <span>/ −{counts.removed.toLocaleString("zh-CN")} / ~{counts.changed.toLocaleString("zh-CN")}</span></strong></article>; })}</div>
    <p className="runtime-diff-impact"><strong>运行影响</strong><span>{diff.impact.dependentPlatformInstanceCount ?? 0} 个游戏目录受到影响；{diff.impact.variantRevalidationCount ?? 0} 个游戏运行版本需要重新检查；{diff.summary.warnings.toLocaleString("zh-CN")} 项解析警告。</span></p>
    <div className="runtime-diff-tabs" aria-label="差异类型">{diffSections.map(([value, label]) => <button type="button" className={section === value ? "is-active" : ""} disabled={busy} onClick={() => onSection(value)} key={value}>{label}</button>)}</div>
    {diff.items.length === 0 ? <p className="runtime-diff-empty">当前类型没有变化。</p> : <div className="runtime-diff-table"><table><thead><tr><th>变化</th><th>对象</th><th>当前</th><th>目标</th></tr></thead><tbody>{diff.items.map((item) => <tr key={`${item.section}:${JSON.stringify(item.key)}`}><td><StatusBadge tone={item.change === "REMOVED" ? "bad" : item.change === "ADDED" ? "good" : "warn"}>{changeLabels[item.change]}</StatusBadge></td><td>{formatRecord(item.key)}</td><td title={formatRecord(item.before)}>{formatRecord(item.before)}</td><td title={formatRecord(item.after)}>{formatRecord(item.after)}</td></tr>)}</tbody></table></div>}
    {diff.nextCursor ? <button type="button" className="button secondary compact" disabled={busy} onClick={onMore}>加载更多差异</button> : null}
  </div>;
}

export function DATManager({ versions, artifacts, initialFilters }: { versions: DATVersion[]; artifacts: CoreArtifact[]; initialFilters?: Partial<DATFilters> }) {
  const router = useRouter();
  const fileInput = useRef<HTMLInputElement>(null);
  const drawerClose = useRef<HTMLButtonElement>(null);
  const drawer = useRef<HTMLElement>(null);
  const availableArtifacts = useMemo(() => artifacts.filter((item) => item.enabled && arcadeCores.has(item.coreId)), [artifacts]);
  const [artifactId, setArtifactId] = useState(availableArtifacts[0]?.id ?? "");
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [filters, setFilters] = useState<DATFilters>({ query: "", source: "", parseStatus: "", quick: "ALL", ...initialFilters });
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [diff, setDiff] = useState<Diff | null>(null);
  const [diffSection, setDiffSection] = useState("MACHINES");
  const [diffIntent, setDiffIntent] = useState<DiffIntent>(null);
  const [deleteItem, setDeleteItem] = useState<DATVersion | null>(null);

  const artifactById = useMemo(() => new Map(artifacts.map((artifact) => [artifact.id, artifact])), [artifacts]);
  const activeVersions = versions.filter((item) => item.active);
  const filteredVersions = filterDATVersions(versions, filters).filter((item) => !item.active);
  const summary = summarizeDAT(versions);

  useEffect(() => {
    if (!drawerOpen) return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    drawerClose.current?.focus();
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape" && busy !== "upload") setDrawerOpen(false); };
    window.addEventListener("keydown", escape);
    return () => { window.removeEventListener("keydown", escape); previous?.focus(); };
  }, [drawerOpen, busy]);

  function patchFilters(patch: Partial<DATFilters>) { setFilters((current) => ({ ...current, ...patch })); }

  async function upload() {
    if (!selectedFile || !artifactId) return;
    setBusy("upload"); setError(""); setNotice("");
    try {
      const uploaded = await uploadOne(selectedFile, (message) => setNotice(message));
      const response = await fetch("/api/v1/admin/arcade-dats", {
        method: "POST", credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }),
        body: JSON.stringify({ uploadFileId: uploaded.uploadFileId, coreArtifactId: artifactId }),
      });
      if (!response.ok) throw new Error(await responseError(response, "无法创建 DAT 候选"));
      const created = await response.json() as { datVersionId: string; jobId: string };
      setNotice("候选已创建，正在由后端安全解析…");
      await waitForJob(created.jobId, (state) => setNotice(`DAT 解析 · ${state}`));
      setNotice("候选已解析，可在版本列表中查看差异后启用。");
      setDrawerOpen(false); setSelectedFile(null); router.refresh();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "DAT 上传失败");
      router.refresh();
    } finally {
      setBusy(null);
      if (fileInput.current) fileInput.current.value = "";
    }
  }

  async function fetchDiff(item: DATVersion) {
    const response = await fetch(`/api/v1/admin/arcade-dats/${item.id}/diff?section=MACHINES&change=ALL&limit=50`, { cache: "no-store" });
    if (!response.ok) throw new Error(await responseError(response, "无法读取 DAT 差异"));
    const next = await response.json() as Diff;
    setDiff(next); setDiffSection("MACHINES");
    return next;
  }

  async function preview(item: DATVersion) {
    setBusy(item.id); setError("");
    try { await fetchDiff(item); setDiffIntent(null); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "DAT 差异读取失败"); }
    finally { setBusy(null); }
  }

  async function requestChange(item: DATVersion, rollback: boolean) {
    setBusy(item.id); setError("");
    try { await fetchDiff(item); setDiffIntent({ item, rollback }); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "DAT 差异读取失败"); }
    finally { setBusy(null); }
  }

  async function loadDiffItems(section: string, append = false) {
    if (!diff) return;
    setBusy(diff.targetDatVersionId); setError("");
    try {
      const cursor = append && diff.nextCursor ? `&cursor=${encodeURIComponent(diff.nextCursor)}` : "";
      const response = await fetch(`/api/v1/admin/arcade-dats/${diff.targetDatVersionId}/diff?section=${section}&change=ALL&limit=50${cursor}`, { cache: "no-store" });
      if (!response.ok) throw new Error(await responseError(response, "无法读取 DAT 差异明细"));
      const next = await response.json() as Diff;
      setDiff({ ...next, items: append ? [...diff.items, ...next.items] : next.items }); setDiffSection(section);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "DAT 差异明细读取失败"); }
    finally { setBusy(null); }
  }

  async function confirmChange() {
    if (!diff || !diffIntent) return;
    const { item, rollback } = diffIntent;
    const artifact = artifactById.get(item.coreArtifactId);
    if (!artifact) { setError("找不到目标运行方式版本"); return; }
    setBusy(item.id); setError("");
    try {
      const response = await fetch(`/api/v1/admin/arcade-dats/${item.id}/${rollback ? "rollback" : "activate"}`, {
        method: "POST", credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${artifact.version}"`, "Idempotency-Key": newUuid() }),
        body: JSON.stringify({ impactDigest: diff.impactDigest, confirmBlocked: (diff.impact.variantRevalidationCount ?? 0) > 0, confirmUnknownCompatibility: item.compatibilityStatus === "UNKNOWN" }),
      });
      if (!response.ok) throw new Error(await responseError(response, `DAT ${rollback ? "回滚" : "启用"}失败`));
      setNotice(`${item.coreName} 数据目录已${rollback ? "恢复" : "启用"}；历史版本和游戏运行快照仍会保留。`);
      setDiff(null); setDiffIntent(null); router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "DAT 状态变更失败"); }
    finally { setBusy(null); }
  }

  async function remove() {
    if (!deleteItem) return;
    setBusy(deleteItem.id); setError("");
    try {
      const response = await fetch(`/api/v1/admin/arcade-dats/${deleteItem.id}`, { method: "DELETE", credentials: "same-origin", headers: await writeHeaders({ "If-Match": `"v${deleteItem.version}"` }) });
      if (!response.ok) throw new Error(await responseError(response, "DAT 候选不可删除"));
      setNotice("未启用的候选数据目录已删除；正在使用或已引用的版本仍受保护。"); setDeleteItem(null); router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "DAT 删除失败"); }
    finally { setBusy(null); }
  }

  async function cancel(item: DATVersion) {
    if (!item.jobId || !item.jobVersion) return;
    setBusy(item.id); setError("");
    try {
      const response = await fetch(`/api/v1/admin/jobs/${item.jobId}/cancel`, { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${item.jobVersion}"`, "Idempotency-Key": newUuid() }), body: JSON.stringify({ reason: "用户从 DAT 管理页取消解析" }) });
      if (!response.ok) throw new Error(await responseError(response, "无法取消 DAT 解析"));
      setNotice("已请求取消；解析会在下一个安全检查点停止。"); router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "取消失败"); }
    finally { setBusy(null); }
  }

  const quickFilters: Array<[DATQuickFilter, string, number]> = [["ALL", "全部", summary.all], ["READY", "可启用", summary.ready], ["WORKING", "处理中", summary.working], ["ATTENTION", "需处理", summary.attention], ["HISTORY", "历史版本", summary.history]];

  return <div className="page-layout page-layout-admin runtime-dependency-page">
    <PageHeader eyebrow="运行依赖" title="街机数据目录" description="管理 Arcade 运行方式当前使用的数据目录，以及候选和历史版本。先确认当前启用版本，再上传、比较或切换。" actions={<><ButtonLink href="/admin/bios" secondary>← BIOS 文件</ButtonLink><button type="button" className="button" onClick={() => setDrawerOpen(true)}><AppIcon name="plus" />上传新目录</button></>} />

    <section className="runtime-section"><div className="runtime-section-heading"><div><h2>当前启用</h2><p>这里展示每个街机运行方式实际生效的数据目录。</p></div><span>{activeVersions.length} 个运行方式</span></div>
      {activeVersions.length ? <div className="runtime-active-grid">{activeVersions.map((item) => <DATActiveCard item={item} artifact={artifactById.get(item.coreArtifactId)} key={item.id} />)}</div> : <div className="runtime-inline-empty compact"><h2>尚无启用的数据目录</h2><p>依赖准备完成后，系统内置基线会在这里显示。</p></div>}
    </section>

    <section className="runtime-toolbar runtime-dat-toolbar panel" aria-label="筛选街机数据目录">
      <label className="runtime-search"><span>搜索运行方式</span><span className="search"><AppIcon name="search" /><input type="search" aria-label="搜索街机运行方式" placeholder="例如 FinalBurn Neo" value={filters.query} onChange={(event) => patchFilters({ query: event.target.value })} /></span></label>
      <label><span>目录来源</span><select className="select" aria-label="目录来源" value={filters.source} onChange={(event) => patchFilters({ source: event.target.value })}><option value="">所有来源</option><option value="BUILTIN">系统内置</option><option value="USER">手动上传</option></select></label>
      <label><span>处理状态</span><select className="select" aria-label="处理状态" value={filters.parseStatus} onChange={(event) => patchFilters({ parseStatus: event.target.value })}><option value="">所有状态</option><option value="READY">解析完成</option><option value="PENDING">等待解析</option><option value="PARSING">正在解析</option><option value="FAILED">解析失败</option><option value="CANCELLED">已取消</option></select></label>
    </section>
    <div className="runtime-chips" aria-label="DAT 快速筛选">{quickFilters.map(([value, label, count]) => <button type="button" className={filters.quick === value ? "is-active" : ""} aria-pressed={filters.quick === value} onClick={() => patchFilters({ quick: value })} key={value}>{label} {count}</button>)}</div>

    <section className="runtime-section"><div className="runtime-section-heading"><div><h2>候选与历史版本</h2><p>只有解析成功的候选可以查看差异并显式启用。</p></div><span>{filteredVersions.length} 个版本</span></div>
      {filteredVersions.length ? <div className="runtime-list" role="table" aria-label="DAT 候选与历史版本">{filteredVersions.map((item) => <article className="runtime-dat-row" role="row" key={item.id}>
        <div className="runtime-dat-main" role="cell"><h3>{item.coreName}</h3><small>{sourceLabel(item.source)} · 更新于 {formatDate(item.updatedAtMs)}</small></div>
        <div role="cell"><span className="runtime-source">{sourceLabel(item.source)}</span></div>
        <div role="cell"><StatusBadge tone={item.source === "BUILTIN" && item.parseStatus === "READY" ? "info" : statusTone(item.parseStatus)}>{stateLabel(item)}</StatusBadge></div>
        <div className="runtime-dat-content" role="cell"><strong>{item.machineCount === null ? "—" : `${item.machineCount.toLocaleString("zh-CN")} 个游戏`}</strong><small>{item.romEntryCount === null ? "解析完成后显示统计" : `${item.romEntryCount.toLocaleString("zh-CN")} 个 ROM 条目`}</small></div>
        <div className="runtime-dat-content" role="cell"><strong>{compatibilityLabel(item.compatibilityStatus)}</strong><small>{item.parseStatus === "READY" ? "可在启用前查看实际变化" : "等待候选完成解析"}</small></div>
        <div className="runtime-row-actions" role="cell">
          {item.parseStatus === "READY" && item.source === "USER" ? <><button type="button" className="button secondary compact" disabled={busy !== null} onClick={() => void preview(item)}>查看差异</button><button type="button" className="button compact" disabled={busy !== null} onClick={() => void requestChange(item, false)}>启用</button></> : null}
          {item.parseStatus === "READY" && item.source === "BUILTIN" ? <button type="button" className="button secondary compact" disabled={busy !== null} onClick={() => void requestChange(item, true)}>预览回滚</button> : null}
          {["PENDING", "PARSING"].includes(item.parseStatus) && item.jobId ? <button type="button" className="button secondary compact" disabled={busy !== null} onClick={() => void cancel(item)}>取消解析</button> : null}
          {item.source === "USER" && !["PENDING", "PARSING"].includes(item.parseStatus) ? <button type="button" className="runtime-delete-button" aria-label={`删除 ${item.coreName} 候选`} disabled={busy !== null} onClick={() => setDeleteItem(item)}>删除</button> : null}
        </div>
      </article>)}</div> : <div className="runtime-inline-empty compact"><h2>没有符合条件的版本</h2><p>调整来源、处理状态或快速筛选后再试。</p></div>}
    </section>

    {drawerOpen ? <><button className="runtime-drawer-backdrop" type="button" aria-label="关闭上传街机数据目录" disabled={busy === "upload"} onClick={() => setDrawerOpen(false)} /><aside ref={drawer} className="runtime-drawer" role="dialog" aria-modal="true" aria-labelledby="runtime-upload-title" onKeyDown={(event) => {
      if (event.key !== "Tab") return;
      const focusable = Array.from(drawer.current?.querySelectorAll<HTMLElement>("button:not(:disabled), input:not(:disabled), select:not(:disabled), [tabindex]:not([tabindex='-1'])") ?? []);
      if (!focusable.length) return;
      const first = focusable[0]; const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    }}><header><div><StatusBadge tone="info">新候选</StatusBadge><h2 id="runtime-upload-title">上传街机数据目录</h2><p>上传后先安全解析，再查看差异，最后才允许启用。</p></div><button ref={drawerClose} type="button" className="runtime-drawer-close" aria-label="关闭" disabled={busy === "upload"} onClick={() => setDrawerOpen(false)}><AppIcon name="x" /></button></header><div className="runtime-drawer-body">
      <label><span>目标运行方式</span><select className="select" aria-label="目标运行方式" value={artifactId} disabled={busy === "upload"} onChange={(event) => setArtifactId(event.target.value)}>{availableArtifacts.map((item) => <option value={item.id} key={item.id}>{item.coreName} · bundle {item.bundleVersion}</option>)}</select></label>
      <label><span>DAT / XML 文件</span><input className="runtime-file-name" value={selectedFile?.name ?? ""} placeholder="尚未选择文件" readOnly /></label>
      <input ref={fileInput} hidden type="file" aria-label="DAT 或 XML 文件" accept=".dat,.xml,text/xml,application/xml" disabled={busy === "upload"} onChange={(event) => setSelectedFile(event.target.files?.[0] ?? null)} />
      <button type="button" className="button secondary" disabled={busy === "upload"} onClick={() => fileInput.current?.click()}>选择 DAT 或 XML 文件</button>
      <ol className="runtime-upload-flow"><li><span>1</span><strong>上传</strong></li><li><span>2</span><strong>解析</strong></li><li><span>3</span><strong>查看差异</strong></li><li><span>4</span><strong>启用</strong></li></ol>
      {busy === "upload" ? <p className="runtime-drawer-progress"><i className="button-spinner" />{notice || "正在上传并解析…"}</p> : null}
    </div><footer><button type="button" className="button secondary" disabled={busy === "upload"} onClick={() => setDrawerOpen(false)}>取消</button><button type="button" className="button" disabled={busy === "upload" || !selectedFile || !artifactId} onClick={() => void upload()}>{busy === "upload" ? "上传并解析中…" : "开始上传"}</button></footer></aside></> : null}

    <ConfirmDialog wide open={diff !== null} title={diffIntent ? `${diffIntent.rollback ? "恢复" : "启用"} ${diffIntent.item.coreName} 数据目录？` : "数据目录差异与运行影响"} description="比较当前启用版本与目标版本；提交时会重新校验本次预览是否过期。" confirmLabel={diffIntent ? (diffIntent.rollback ? "恢复这个版本" : "启用这个版本") : "关闭"} hideCancel={!diffIntent} busy={busy !== null} onCancel={() => { setDiff(null); setDiffIntent(null); }} onConfirm={() => diffIntent ? void confirmChange() : (setDiff(null), setDiffIntent(null))}>{diff ? <DATDiffContent diff={diff} versions={versions} busy={busy !== null} section={diffSection} onSection={(section) => void loadDiffItems(section)} onMore={() => void loadDiffItems(diffSection, true)} /> : null}</ConfirmDialog>
    <ConfirmDialog open={deleteItem !== null} title="删除这个候选数据目录？" description="未启用的用户候选会从列表中移除。" confirmLabel="删除候选" tone="danger" busy={busy !== null} onCancel={() => setDeleteItem(null)} onConfirm={() => void remove()}><ul><li>正在使用或已经被游戏引用的版本仍受保护</li><li>系统内置版本不能删除</li></ul></ConfirmDialog>
    {notice || error ? <div className={`app-toast ${error ? "bad" : "good"}`} role={error ? "alert" : "status"}><span>{error || notice}</span><button type="button" aria-label="关闭提示" onClick={() => { setNotice(""); setError(""); }}>×</button></div> : null}
  </div>;
}
