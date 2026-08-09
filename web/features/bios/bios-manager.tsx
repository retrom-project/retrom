"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast } from "@/components/flash-toast";
import { StatusBadge } from "@/components/ui";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import { responseError, uploadOne } from "@/lib/upload";
import {
  filterBIOS,
  isBIOSAttention,
  summarizeBIOS,
  type BIOSFilters,
  type BIOSQuickFilter,
  type BIOSRequirement,
} from "./runtime-dependencies";

export type { BIOSRequirement } from "./runtime-dependencies";

type Scope = "REQUIRED_BY_LIBRARY" | "FULL_CATALOG";

const statusLabels: Record<string, string> = {
  MATCHED: "已安装并匹配",
  MISSING: "缺少文件",
  MISMATCHED: "文件不匹配",
  HASH_WARNING: "校验值不一致",
  MISSING_ENTRY: "归档内缺少文件",
  OPTIONAL_MISSING: "可选文件未安装",
  INVALID: "文件无效",
  SATISFIED_BY_CONTENT: "由游戏内容满足",
  UNVERIFIED: "等待验证",
};

const requirementLabels: Record<string, string> = { REQUIRED: "必需", OPTIONAL: "可选", CONDITIONAL: "按需" };

type ArchiveEntryFacts = { name: string; sizeBytes: number; crc32?: string };
type ArchiveEntryComparison = {
  status: "MATCHED" | "ALIASED" | "MISMATCHED" | "MISSING" | "EXTRA";
  expected: ArchiveEntryFacts | null;
  actual: ArchiveEntryFacts | null;
};
type ArchiveInspection = {
  requirementId: string;
  logicalName: string;
  installationId: string;
  installationStatus: string;
  entries: ArchiveEntryComparison[];
};

const entryStatusLabels: Record<ArchiveEntryComparison["status"], string> = {
  MATCHED: "匹配",
  ALIASED: "内容匹配·文件名不同",
  MISMATCHED: "校验不一致",
  MISSING: "ZIP 内缺失",
  EXTRA: "ZIP 内额外文件",
};

function tone(status: string): "good" | "warn" | "bad" {
  if (["MATCHED", "SATISFIED_BY_CONTENT"].includes(status)) return "good";
  if (["MISSING", "MISSING_ENTRY", "INVALID"].includes(status)) return "bad";
  return "warn";
}

function updateURL(scope: Scope, filters: BIOSFilters) {
  const params = new URLSearchParams(window.location.search);
  const values: Record<string, string> = { scope, q: filters.query.trim(), coreId: filters.coreId, status: filters.status };
  for (const [name, value] of Object.entries(values)) {
    if (value) params.set(name, value);
    else params.delete(name);
  }
  const query = params.toString();
  window.history.replaceState(window.history.state, "", `${window.location.pathname}${query ? `?${query}` : ""}`);
}

function BIOSRow({ item, currentLibrary, busy, inputRef, onInstall, onInspect }: {
  item: BIOSRequirement;
  currentLibrary: boolean;
  busy: string | null;
  inputRef: (element: HTMLInputElement | null) => void;
  onInstall: (item: BIOSRequirement, file: File) => void;
  onInspect: (item: BIOSRequirement) => void;
}) {
  const installed = item.activeInstallation;
  const canUpload = item.status !== "SATISFIED_BY_CONTENT";
  return <article className="runtime-bios-row" role="row">
    <div className="runtime-bios-file" role="cell">
      <span className="runtime-file-mark" aria-hidden="true">{item.logicalName.toLowerCase().endsWith(".zip") ? "ZIP" : "BIOS"}</span>
      <div><h3>{item.sourceKind === "DAT_MACHINE" && installed ? <button className="runtime-bios-inspect" type="button" onClick={() => onInspect(item)}>{item.logicalName}</button> : item.logicalName}</h3><p>{requirementLabels[item.requirementMode] ?? item.requirementMode}{item.conditionCode ? " · 按游戏内容决定是否需要" : ""}</p>
        {(item.expectedMd5 || installed?.md5) ? <dl className="runtime-technical">
          {item.expectedMd5 ? <><dt>期望 MD5</dt><dd><code>{item.expectedMd5}</code></dd></> : null}
          {installed?.md5 ? <><dt>当前 MD5</dt><dd><code>{installed.md5}</code></dd></> : null}
        </dl> : null}
      </div>
    </div>
    <div className="runtime-core" role="cell"><strong>{item.coreName}</strong><small>{item.coreId}</small></div>
    <div role="cell"><StatusBadge tone={tone(item.status)}>{statusLabels[item.status] ?? item.status}</StatusBadge></div>
    <div className="runtime-usage" role="cell"><strong>{currentLibrary ? (isBIOSAttention(item) ? "当前游戏库需要处理" : "当前游戏库已就绪") : "完整核心目录项"}</strong><small>{item.requirementMode === "OPTIONAL" ? "未安装不会作为必需依赖阻断" : item.status === "HASH_WARNING" ? "校验警告允许启动，但建议核对文件" : "启动前会按当前运行方式检查"}</small></div>
    <div className="runtime-row-actions" role="cell">
      {canUpload ? <><input ref={inputRef} hidden id={`bios-${item.id}`} type="file" disabled={busy !== null} onChange={(event) => { const file = event.target.files?.[0]; if (file) onInstall(item, file); }} /><button className={`button ${isBIOSAttention(item) ? "" : "secondary"} compact`} type="button" disabled={busy !== null} onClick={() => document.getElementById(`bios-${item.id}`)?.click()}>{busy === item.id ? "验证中…" : installed ? "替换文件" : "选择 BIOS 文件"}</button></> : <span className="runtime-no-action">无需上传</span>}
    </div>
  </article>;
}

export function BIOSManager({ libraryItems, catalogItems, initialScope = "REQUIRED_BY_LIBRARY", initialFilters }: {
  libraryItems: BIOSRequirement[];
  catalogItems: BIOSRequirement[];
  initialScope?: Scope;
  initialFilters?: Partial<BIOSFilters>;
}) {
  const router = useRouter();
  const inputs = useRef<Record<string, HTMLInputElement | null>>({});
  const [scope, setScope] = useState<Scope>(initialScope);
  const [filters, setFilters] = useState<BIOSFilters>({ query: "", coreId: "", status: "", quick: "ALL", ...initialFilters });
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [archiveDialog, setArchiveDialog] = useState<{ item: BIOSRequirement; loading: boolean; error: string; inspection: ArchiveInspection | null } | null>(null);

  const libraryIds = useMemo(() => new Set(libraryItems.map((item) => item.id)), [libraryItems]);
  const scopedItems = scope === "REQUIRED_BY_LIBRARY" ? libraryItems : catalogItems;
  const summary = summarizeBIOS(scopedItems);
  const filtered = filterBIOS(scopedItems, filters);
  const attention = filtered.filter(isBIOSAttention);
  const ready = filtered.filter((item) => !isBIOSAttention(item));
  const cores = useMemo(() => [...new Map(catalogItems.map((item) => [item.coreId, item.coreName])).entries()].sort((left, right) => left[1].localeCompare(right[1], "zh-CN")), [catalogItems]);

  useEffect(() => updateURL(scope, filters), [scope, filters]);

  function patchFilters(patch: Partial<BIOSFilters>) {
    setFilters((current) => ({ ...current, ...patch }));
  }

  async function install(requirement: BIOSRequirement, file: File) {
    setBusy(requirement.id); setError(""); setNotice("");
    try {
      const upload = await uploadOne(file, (message) => setNotice(message));
      setNotice("正在验证 BIOS 内容并保存安装记录…");
      const response = await fetch(`/api/v1/admin/bios/${requirement.id}/installations`, {
        method: "POST",
        credentials: "same-origin",
        headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${requirement.version}"`, "Idempotency-Key": newUuid() }),
        body: JSON.stringify({ uploadFileId: upload.uploadFileId }),
      });
      if (!response.ok) throw new Error(await responseError(response, "BIOS 安装失败"));
      const installed = await response.json() as { status: string };
      setNotice(`BIOS 已安装：${statusLabels[installed.status] ?? "验证完成"}`);
      router.refresh();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "BIOS 安装失败");
    } finally {
      setBusy(null);
      const input = inputs.current[requirement.id];
      if (input) input.value = "";
    }
  }

  async function inspectArchive(requirement: BIOSRequirement) {
    setArchiveDialog({ item: requirement, loading: true, error: "", inspection: null });
    try {
      const response = await fetch(`/api/v1/admin/bios/${requirement.id}/entries`, { credentials: "same-origin" });
      if (!response.ok) throw new Error(await responseError(response, "BIOS 条目读取失败"));
      const inspection = await response.json() as ArchiveInspection;
      setArchiveDialog((current) => current?.item.id === requirement.id ? { ...current, loading: false, inspection } : current);
    } catch (caught) {
      const message = caught instanceof Error ? caught.message : "BIOS 条目读取失败";
      setArchiveDialog((current) => current?.item.id === requirement.id ? { ...current, loading: false, error: message } : current);
    }
  }

  const quickFilters: Array<[BIOSQuickFilter, string, number]> = [
    ["ALL", "全部", scopedItems.length],
    ["ATTENTION", "需要处理", scopedItems.filter(isBIOSAttention).length],
    ["REQUIRED", "必需", scopedItems.filter((item) => item.requirementMode === "REQUIRED").length],
    ["OPTIONAL", "可选", scopedItems.filter((item) => item.requirementMode === "OPTIONAL").length],
  ];

  return <div className="runtime-dependency-page">
    <div className="runtime-segment" role="group" aria-label="BIOS 查看范围">
      <button type="button" className={scope === "REQUIRED_BY_LIBRARY" ? "is-active" : ""} aria-pressed={scope === "REQUIRED_BY_LIBRARY"} onClick={() => setScope("REQUIRED_BY_LIBRARY")}>当前游戏库需要 <strong>{libraryItems.length}</strong></button>
      <button type="button" className={scope === "FULL_CATALOG" ? "is-active" : ""} aria-pressed={scope === "FULL_CATALOG"} onClick={() => setScope("FULL_CATALOG")}>完整 BIOS 目录 <strong>{catalogItems.length}</strong></button>
    </div>

    <section className="runtime-kpis" aria-label="BIOS 依赖摘要">
      <article><small>当前范围</small><strong>{summary.total}</strong><p>{scope === "REQUIRED_BY_LIBRARY" ? "游戏库实际引用的依赖" : "全部已支持核心的目录"}</p></article>
      <article className={summary.blocking ? "has-danger" : ""}><small>缺失 / 阻断</small><strong>{summary.blocking}</strong><p>必需文件缺失会阻断相关游戏</p></article>
      <article className={summary.warnings ? "has-warning" : ""}><small>需要核对</small><strong>{summary.warnings}</strong><p>哈希不同仍可启动，建议替换</p></article>
      <article className="has-success"><small>已就绪</small><strong>{summary.ready}</strong><p>已经安装并通过当前校验</p></article>
    </section>

    <section className="runtime-toolbar panel" aria-label="筛选 BIOS 文件">
      <label className="runtime-search"><span>搜索文件或运行方式</span><span className="search"><AppIcon name="search" /><input type="search" aria-label="搜索 BIOS 文件" placeholder="例如 gba_bios.bin 或 mGBA" value={filters.query} onChange={(event) => patchFilters({ query: event.target.value })} /></span></label>
      <label><span>运行方式</span><select className="select" aria-label="运行方式" value={filters.coreId} onChange={(event) => patchFilters({ coreId: event.target.value })}><option value="">全部运行方式</option>{cores.map(([id, name]) => <option value={id} key={id}>{name}</option>)}</select></label>
      <label><span>文件状态</span><select className="select" aria-label="文件状态" value={filters.status} onChange={(event) => patchFilters({ status: event.target.value })}><option value="">所有状态</option><option value="MISSING">缺少文件</option><option value="MISSING_ENTRY">归档不完整</option><option value="HASH_WARNING">校验值不一致</option><option value="MATCHED">已安装并匹配</option><option value="OPTIONAL_MISSING">可选文件未安装</option></select></label>
    </section>

    <div className="runtime-chips" aria-label="BIOS 快速筛选">{quickFilters.map(([value, label, count]) => <button type="button" className={filters.quick === value ? "is-active" : ""} aria-pressed={filters.quick === value} onClick={() => patchFilters({ quick: value })} key={value}>{label} {count}</button>)}</div>

    {filtered.length === 0 ? <section className="runtime-inline-empty"><h2>没有符合条件的 BIOS 文件</h2><p>调整查看范围或清除筛选条件后再试。</p><button type="button" className="button secondary compact" onClick={() => setFilters({ query: "", coreId: "", status: "", quick: "ALL" })}>清除筛选</button></section> : <>
      {attention.length ? <section className="runtime-section"><div className="runtime-section-heading"><div><h2>需要处理</h2><p>优先展示会阻断游戏或需要管理员核对的依赖。</p></div><span>{attention.length} 项</span></div><div className="runtime-list" role="table" aria-label="需要处理的 BIOS 文件">{attention.map((item) => <BIOSRow item={item} currentLibrary={libraryIds.has(item.id)} busy={busy} inputRef={(element) => { inputs.current[item.id] = element; }} onInstall={(requirement, file) => void install(requirement, file)} onInspect={(requirement) => void inspectArchive(requirement)} key={item.id} />)}</div></section> : null}
      {ready.length ? <section className="runtime-section"><div className="runtime-section-heading"><div><h2>已就绪与可选项</h2><p>这些依赖当前不会阻断游戏运行。</p></div><span>{ready.length} 项</span></div><div className="runtime-list" role="table" aria-label="已就绪的 BIOS 文件">{ready.map((item) => <BIOSRow item={item} currentLibrary={libraryIds.has(item.id)} busy={busy} inputRef={(element) => { inputs.current[item.id] = element; }} onInstall={(requirement, file) => void install(requirement, file)} onInspect={(requirement) => void inspectArchive(requirement)} key={item.id} />)}</div></section> : null}
    </>}
    <ConfirmDialog
      open={archiveDialog !== null}
      title={`${archiveDialog?.item.logicalName ?? "BIOS"} 内容对比`}
      description="左侧为当前 DAT 要求，右侧为已安装 ZIP 内容；每个文件固定展示 name、size、crc 三项。额外文件不阻断启动。"
      confirmLabel="关闭"
      hideCancel
      wide
      onConfirm={() => setArchiveDialog(null)}
      onCancel={() => setArchiveDialog(null)}
    >
      {archiveDialog?.loading ? <p className="bios-entry-message">正在读取已安装 BIOS 的安全扫描结果…</p> : archiveDialog?.error ? <p className="bios-entry-message is-error">{archiveDialog.error}</p> : archiveDialog?.inspection ? <ArchiveComparisonLists inspection={archiveDialog.inspection} /> : null}
    </ConfirmDialog>
    <Toast toast={error ? { message: error, tone: "bad" } : notice ? { message: notice, tone: "good" } : null} onDismiss={() => { setNotice(""); setError(""); }} />
  </div>;
}

function ArchiveComparisonLists({ inspection }: { inspection: ArchiveInspection }) {
  const expectedEntries = inspection.entries.filter((entry): entry is ArchiveEntryComparison & { expected: ArchiveEntryFacts } => entry.expected !== null);
  const actualEntries = inspection.entries.filter((entry): entry is ArchiveEntryComparison & { actual: ArchiveEntryFacts } => entry.actual !== null);
  return <div className="bios-entry-comparison">
    <ArchiveEntryList title="DAT 要求" ariaLabel="DAT 要求列表" entries={expectedEntries.map((entry) => ({ facts: entry.expected, status: entry.status }))} />
    <ArchiveEntryList title="当前 ZIP 内容" ariaLabel="当前 ZIP 内容列表" entries={actualEntries.map((entry) => ({ facts: entry.actual, status: entry.status }))} />
  </div>;
}

function ArchiveEntryList({ title, ariaLabel, entries }: {
  title: string;
  ariaLabel: string;
  entries: Array<{ facts: ArchiveEntryFacts; status: ArchiveEntryComparison["status"] }>;
}) {
  return <section className="bios-entry-column">
    <header><strong>{title}</strong><span>{entries.length} 项</span></header>
    <ul className="bios-entry-list" aria-label={ariaLabel}>{entries.map((entry, index) => <li className={`bios-entry-card is-${entry.status.toLowerCase()}`} key={`${entry.facts.name}-${index}`}>
      <div className="bios-entry-card-head"><StatusBadge tone={entry.status === "MATCHED" || entry.status === "ALIASED" ? "good" : entry.status === "MISMATCHED" || entry.status === "EXTRA" ? "warn" : "bad"}>{entryStatusLabels[entry.status]}</StatusBadge></div>
      <ArchiveFacts facts={entry.facts} />
    </li>)}</ul>
  </section>;
}

function ArchiveFacts({ facts }: { facts: ArchiveEntryFacts }) {
  return <ul className="bios-entry-facts" aria-label={`${facts.name} 文件信息`}>
    <li><span>name</span><code>{facts.name}</code></li>
    <li><span>size</span><strong>{facts.sizeBytes.toLocaleString("zh-CN")} bytes</strong></li>
    <li><span>crc</span><code>{facts.crc32 || "—"}</code></li>
  </ul>;
}
