"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState, type ChangeEvent, type RefObject } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast } from "@/components/flash-toast";
import { StatusBadge } from "@/components/ui";
import { api, writeHeaders } from "@/lib/api/client";
import type { components } from "@/lib/api/generated/schema";
import { newUuid } from "@/lib/crypto";
import { responseError, uploadOne } from "@/lib/upload";
import { isBIOSAttention, type BIOSFilters, type BIOSQuickFilter } from "./runtime-dependencies";

export type BIOSRequirement = components["schemas"]["BIOSRequirementSummary"];
export type BIOSListResponse = components["schemas"]["BIOSListResponseBody"];

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
  if (["MATCHED", "SATISFIED_BY_CONTENT"].includes(status)) {return "good";}
  if (["MISSING", "MISSING_ENTRY", "INVALID"].includes(status)) {return "bad";}
  return "warn";
}

function updateURL(scope: Scope, filters: BIOSFilters) {
  const params = new URLSearchParams(window.location.search);
  const values: Record<string, string> = { scope, q: filters.query.trim(), coreId: filters.coreId, status: filters.status, quick: filters.quick === "ALL" ? "" : filters.quick };
  for (const [name, value] of Object.entries(values)) {
    if (value) {params.set(name, value);}
    else {params.delete(name);}
  }
  const query = params.toString();
  window.history.replaceState(window.history.state, "", `${window.location.pathname}${query ? `?${query}` : ""}`);
}

function queryKey(scope: Scope, filters: BIOSFilters) {
  return JSON.stringify([scope, filters.query.trim(), filters.coreId, filters.status, filters.quick]);
}

function filtersFromLocation(): { scope: Scope; filters: BIOSFilters } {
  const params = new URLSearchParams(window.location.search);
  const quick = params.get("quick");
  return {
    scope: params.get("scope") === "FULL_CATALOG" ? "FULL_CATALOG" : "REQUIRED_BY_LIBRARY",
    filters: {
      query: params.get("q") ?? "",
      coreId: params.get("coreId") ?? "",
      status: params.get("status") ?? "",
      quick: quick === "ATTENTION" || quick === "REQUIRED" || quick === "OPTIONAL" ? quick : "ALL",
    },
  };
}

type BIOSRowProps = {
  item: BIOSRequirement;
  currentLibrary: boolean;
  busy: string | null;
  inputRef: (element: HTMLInputElement | null) => void;
  onInstall: (item: BIOSRequirement, file: File) => void;
  onInspect: (item: BIOSRequirement) => void;
};

function BIOSFileCell({ installed, item, onInspect }: {
  installed: BIOSRequirement["activeInstallation"];
  item: BIOSRequirement;
  onInspect: BIOSRowProps["onInspect"];
}) {
  const name = item.sourceKind === "DAT_MACHINE" && installed
    ? <button className="runtime-bios-inspect" type="button" onClick={() => onInspect(item)}>{item.logicalName}</button>
    : item.logicalName;
  return <div className="runtime-bios-file" role="cell">
    <span className="runtime-file-mark" aria-hidden="true">{item.logicalName.toLowerCase().endsWith(".zip") ? "ZIP" : "BIOS"}</span>
    <div><h3>{name}</h3><p>{requirementLabels[item.requirementMode] ?? item.requirementMode}{item.conditionCode ? " · 按游戏内容决定是否需要" : ""}</p>
      {(item.expectedMd5 || installed?.md5) ? <dl className="runtime-technical">
        {item.expectedMd5 ? <><dt>期望 MD5</dt><dd><code>{item.expectedMd5}</code></dd></> : null}
        {installed?.md5 ? <><dt>当前 MD5</dt><dd><code>{installed.md5}</code></dd></> : null}
      </dl> : null}
    </div>
  </div>;
}

function BIOSUsage({ currentLibrary, item }: { currentLibrary: boolean; item: BIOSRequirement }) {
  const headline = currentLibrary
    ? (isBIOSAttention(item) ? "当前游戏库需要处理" : "当前游戏库已就绪")
    : "完整核心目录项";
  const detail = item.requirementMode === "OPTIONAL"
    ? "未安装不会作为必需依赖阻断"
    : item.status === "HASH_WARNING" ? "校验警告允许启动，但建议核对文件" : "启动前会按当前运行方式检查";
  return <div className="runtime-usage" role="cell"><strong>{headline}</strong><small>{detail}</small></div>;
}

function BIOSRowAction({ busy, inputRef, installed, item, onInstall }: {
  busy: string | null;
  inputRef: BIOSRowProps["inputRef"];
  installed: BIOSRequirement["activeInstallation"];
  item: BIOSRequirement;
  onInstall: BIOSRowProps["onInstall"];
}) {
  const chooseFile = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (file) {onInstall(item, file);}
  };
  return <div className="runtime-row-actions" role="cell">
    <input ref={inputRef} hidden id={`bios-${item.id}`} type="file" disabled={busy !== null} onChange={chooseFile} />
    <button className={`button ${isBIOSAttention(item) ? "" : "secondary"} compact`} type="button" disabled={busy !== null} onClick={() => document.getElementById(`bios-${item.id}`)?.click()}>{busy === item.id ? "验证中…" : installed ? "替换文件" : "选择 BIOS 文件"}</button>
    {installed ? <small>替换会清理依赖旧 BIOS 的存档与运行会话</small> : null}
  </div>;
}

function BIOSRow({ item, currentLibrary, busy, inputRef, onInstall, onInspect }: BIOSRowProps) {
  const installed = item.activeInstallation;
  return <article className="runtime-bios-row" role="row">
    <BIOSFileCell installed={installed} item={item} onInspect={onInspect} />
    <div className="runtime-core" role="cell"><strong>{item.coreName}</strong><small>{item.coreId}</small></div>
    <div role="cell"><StatusBadge tone={tone(item.status)}>{statusLabels[item.status] ?? item.status}</StatusBadge></div>
    <BIOSUsage currentLibrary={currentLibrary} item={item} />
    <BIOSRowAction busy={busy} inputRef={inputRef} installed={installed} item={item} onInstall={onInstall} />
  </article>;
}

type BIOSResultsProps = {
  announcement: string;
  attention: BIOSRequirement[];
  busy: string | null;
  firstError: string;
  firstLoading: boolean;
  inputs: RefObject<Record<string, HTMLInputElement | null>>;
  loadMore: () => Promise<void>;
  nextError: string;
  nextLoading: boolean;
  onClear: () => void;
  onInstall: BIOSRowProps["onInstall"];
  onInspect: BIOSRowProps["onInspect"];
  onReload: () => Promise<void>;
  ready: BIOSRequirement[];
  response: BIOSListResponse;
  scope: Scope;
  sentinel: RefObject<HTMLDivElement | null>;
};

function BIOSSection({ busy, currentLibrary, description, inputs, items, label, onInstall, onInspect, title }: {
  busy: string | null;
  currentLibrary: boolean;
  description: string;
  inputs: BIOSResultsProps["inputs"];
  items: BIOSRequirement[];
  label: string;
  onInstall: BIOSRowProps["onInstall"];
  onInspect: BIOSRowProps["onInspect"];
  title: string;
}) {
  if (!items.length) {return null;}
  return <section className="runtime-section">
    <div className="runtime-section-heading"><div><h2>{title}</h2><p>{description}</p></div><span>{items.length} 项已加载</span></div>
    <div className="runtime-list" role="table" aria-label={label}>{items.map((item) => <BIOSRow item={item} currentLibrary={currentLibrary} busy={busy} inputRef={(element) => { inputs.current[item.id] = element; }} onInstall={onInstall} onInspect={onInspect} key={item.id} />)}</div>
  </section>;
}

function BIOSPagination({ announcement, loadMore, nextError, nextLoading, response, sentinel }: Pick<BIOSResultsProps, "announcement" | "loadMore" | "nextError" | "nextLoading" | "response" | "sentinel">) {
  const progress = response.nextCursor
    ? `已加载 ${response.items.length} / ${response.filteredCount} 项`
    : `已加载全部 ${response.filteredCount} 项`;
  const buttonLabel = nextLoading ? "正在加载下一批…" : nextError ? "重试加载下一页" : "加载更多";
  return <>
    <div className="runtime-pagination" ref={sentinel}>
      <p>{progress}</p>
      {nextError ? <p className="runtime-pagination-error">{nextError}</p> : null}
      {response.nextCursor ? <button type="button" className="button secondary compact" disabled={nextLoading} onClick={() => void loadMore()}>{buttonLabel}</button> : null}
    </div>
    <p className="sr-only" aria-live="polite">{announcement}</p>
  </>;
}

function LoadedBIOSResults(props: BIOSResultsProps) {
  const currentLibrary = props.scope === "REQUIRED_BY_LIBRARY";
  return <>
    <BIOSSection busy={props.busy} currentLibrary={currentLibrary} description="优先展示会阻断游戏或需要管理员核对的依赖。" inputs={props.inputs} items={props.attention} label="需要处理的 BIOS 文件" onInstall={props.onInstall} onInspect={props.onInspect} title="需要处理" />
    <BIOSSection busy={props.busy} currentLibrary={currentLibrary} description="这些依赖当前不会阻断游戏运行。" inputs={props.inputs} items={props.ready} label="已就绪的 BIOS 文件" onInstall={props.onInstall} onInspect={props.onInspect} title="已就绪与可选项" />
    <BIOSPagination announcement={props.announcement} loadMore={props.loadMore} nextError={props.nextError} nextLoading={props.nextLoading} response={props.response} sentinel={props.sentinel} />
  </>;
}

function BIOSResults(props: BIOSResultsProps) {
  if (props.firstLoading) {return <section className="runtime-inline-empty" role="status"><span className="button-spinner" /><h2>正在加载 BIOS 目录</h2></section>;}
  if (props.firstError) {return <section className="runtime-inline-empty"><h2>BIOS 列表加载失败</h2><p>{props.firstError}</p><button type="button" className="button secondary compact" onClick={() => void props.onReload()}>重试</button></section>;}
  if (props.response.items.length === 0) {return <section className="runtime-inline-empty"><h2>没有符合条件的 BIOS 文件</h2><p>调整查看范围或清除筛选条件后再试。</p><button type="button" className="button secondary compact" onClick={props.onClear}>清除筛选</button></section>;}
  return <LoadedBIOSResults {...props} />;
}

function ArchiveInspectionDialog({ archiveDialog, onClose }: {
  archiveDialog: { item: BIOSRequirement; loading: boolean; error: string; inspection: ArchiveInspection | null } | null;
  onClose: () => void;
}) {
  let content = null;
  if (archiveDialog?.loading) {content = <p className="bios-entry-message">正在读取已安装 BIOS 的安全扫描结果…</p>;}
  else if (archiveDialog?.error) {content = <p className="bios-entry-message is-error">{archiveDialog.error}</p>;}
  else if (archiveDialog?.inspection) {content = <ArchiveComparisonLists inspection={archiveDialog.inspection} />;}
  return <ConfirmDialog
    open={archiveDialog !== null}
    title={`${archiveDialog?.item.logicalName ?? "BIOS"} 内容对比`}
    description="左侧为当前 DAT 要求，右侧为已安装 ZIP 内容；name、size、crc 表头统一位于列表上方。行背景表示状态，悬停可查看说明。"
    confirmLabel="关闭"
    hideCancel
    wide
    onConfirm={onClose}
    onCancel={onClose}
  >{content}</ConfirmDialog>;
}

export function BIOSManager({ initialResponse, initialScope = "REQUIRED_BY_LIBRARY", initialFilters }: {
  initialResponse: BIOSListResponse;
  initialScope?: Scope;
  initialFilters?: Partial<BIOSFilters>;
}) {
  const inputs = useRef<Record<string, HTMLInputElement | null>>({});
  const sentinel = useRef<HTMLDivElement | null>(null);
  const sequence = useRef(0);
  const nextRequest = useRef(false);
  const [scope, setScope] = useState<Scope>(initialScope);
  const [filters, setFilters] = useState<BIOSFilters>({ query: "", coreId: "", status: "", quick: "ALL", ...initialFilters });
  const [response, setResponse] = useState(initialResponse);
  const [loadedKey, setLoadedKey] = useState(() => queryKey(initialScope, { query: "", coreId: "", status: "", quick: "ALL", ...initialFilters }));
  const [firstLoading, setFirstLoading] = useState(false);
  const [firstError, setFirstError] = useState("");
  const [nextLoading, setNextLoading] = useState(false);
  const [nextError, setNextError] = useState("");
  const [announcement, setAnnouncement] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [archiveDialog, setArchiveDialog] = useState<{ item: BIOSRequirement; loading: boolean; error: string; inspection: ArchiveInspection | null } | null>(null);

  const attention = response.items.filter(isBIOSAttention);
  const ready = response.items.filter((item) => !isBIOSAttention(item));
  const cores = useMemo(() => [...new Map(response.items.map((item) => [item.coreId, item.coreName])).entries()].sort((left, right) => left[1].localeCompare(right[1], "zh-CN")), [response.items]);

  useEffect(() => updateURL(scope, filters), [scope, filters]);

  useEffect(() => {
    const restore = () => {
      const next = filtersFromLocation();
      setScope(next.scope);
      setFilters(next.filters);
    };
    window.addEventListener("popstate", restore);
    return () => window.removeEventListener("popstate", restore);
  }, []);

  const requestPage = useCallback(async (cursor: string | null, signal?: AbortSignal) => {
    const { data, response: raw } = await api.GET("/api/v1/admin/bios", { params: { query: {
      scope, q: filters.query.trim() || undefined, coreId: filters.coreId || undefined,
      status: filters.status || undefined, quick: filters.quick, cursor: cursor ?? undefined, limit: 100,
    } }, signal });
    if (!data) {throw new Error(await responseError(raw, "BIOS 列表读取失败"));}
    return data;
  }, [filters, scope]);

  const reloadFirst = useCallback(async () => {
    const current = ++sequence.current;
    setFirstLoading(true); setFirstError(""); setNextError("");
    try {
      const data = await requestPage(null);
      if (current !== sequence.current) {return;}
      setResponse(data); setLoadedKey(queryKey(scope, filters));
    } catch (caught) {
      if (current !== sequence.current || caught instanceof DOMException && caught.name === "AbortError") {return;}
      setFirstError(caught instanceof Error ? caught.message : "BIOS 列表读取失败");
    } finally {
      if (current === sequence.current) {setFirstLoading(false);}
    }
  }, [filters, requestPage, scope]);

  useEffect(() => {
    const key = queryKey(scope, filters);
    if (key === loadedKey) {return;}
    const controller = new AbortController();
    const current = ++sequence.current;
    const load = async () => {
      setFirstLoading(true); setFirstError(""); setNextError("");
      try {
        const data = await requestPage(null, controller.signal);
        if (current !== sequence.current) {return;}
        setResponse(data); setLoadedKey(key);
      } catch (caught) {
        if (current !== sequence.current || caught instanceof DOMException && caught.name === "AbortError") {return;}
        setFirstError(caught instanceof Error ? caught.message : "BIOS 列表读取失败");
      } finally {
        if (current === sequence.current) {setFirstLoading(false);}
      }
    };
    void load();
    return () => controller.abort();
  }, [filters, loadedKey, requestPage, scope]);

  const loadMore = useCallback(async () => {
    const cursor = response.nextCursor;
    if (!cursor || nextRequest.current) {return;}
    nextRequest.current = true; setNextLoading(true); setNextError("");
    const current = sequence.current;
    try {
      const page = await requestPage(cursor);
      if (current !== sequence.current) {return;}
      setResponse((previous) => {
        const ids = new Set(previous.items.map((item) => item.id));
        const appended = page.items.filter((item) => !ids.has(item.id));
        setAnnouncement(`新增 ${appended.length} 项，已加载 ${previous.items.length + appended.length} / ${page.filteredCount} 项`);
        return { ...page, items: [...previous.items, ...appended] };
      });
    } catch (caught) {
      if (current === sequence.current) {setNextError(caught instanceof Error ? caught.message : "下一页读取失败");}
    } finally {
      nextRequest.current = false; setNextLoading(false);
    }
  }, [requestPage, response.nextCursor]);

  useEffect(() => {
    if (!response.nextCursor || typeof IntersectionObserver === "undefined") {return;}
    const observer = new IntersectionObserver((entries) => { if (entries.some((entry) => entry.isIntersecting)) {void loadMore();} }, { rootMargin: "600px 0px" });
    if (sentinel.current) {observer.observe(sentinel.current);}
    return () => observer.disconnect();
  }, [loadMore, response.nextCursor]);

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
      if (!response.ok) {throw new Error(await responseError(response, "BIOS 安装失败"));}
      const installed = await response.json() as { status: string };
      setNotice(`BIOS 已安装：${statusLabels[installed.status] ?? "验证完成"}`);
      await reloadFirst();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "BIOS 安装失败");
    } finally {
      setBusy(null);
      const input = inputs.current[requirement.id];
      if (input) {input.value = "";}
    }
  }

  async function inspectArchive(requirement: BIOSRequirement) {
    setArchiveDialog({ item: requirement, loading: true, error: "", inspection: null });
    try {
      const response = await fetch(`/api/v1/admin/bios/${requirement.id}/entries`, { credentials: "same-origin" });
      if (!response.ok) {throw new Error(await responseError(response, "BIOS 条目读取失败"));}
      const inspection = await response.json() as ArchiveInspection;
      setArchiveDialog((current) => current?.item.id === requirement.id ? { ...current, loading: false, inspection } : current);
    } catch (caught) {
      const message = caught instanceof Error ? caught.message : "BIOS 条目读取失败";
      setArchiveDialog((current) => current?.item.id === requirement.id ? { ...current, loading: false, error: message } : current);
    }
  }

  const quickFilters: Array<[BIOSQuickFilter, string, number]> = [
    ["ALL", "全部", response.summary.totalCount],
    ["ATTENTION", "需要处理", response.summary.attentionCount],
    ["REQUIRED", "必需", response.summary.requiredCount],
    ["OPTIONAL", "可选", response.summary.optionalCount],
  ];

  return <div className="runtime-dependency-page">
    <div className="runtime-segment" role="group" aria-label="BIOS 查看范围">
      <button type="button" className={scope === "REQUIRED_BY_LIBRARY" ? "is-active" : ""} aria-pressed={scope === "REQUIRED_BY_LIBRARY"} onClick={() => setScope("REQUIRED_BY_LIBRARY")}>当前游戏库需要 <strong>{response.scopeCounts.requiredByLibrary}</strong></button>
      <button type="button" className={scope === "FULL_CATALOG" ? "is-active" : ""} aria-pressed={scope === "FULL_CATALOG"} onClick={() => setScope("FULL_CATALOG")}>完整 BIOS 目录 <strong>{response.scopeCounts.fullCatalog}</strong></button>
    </div>

    <section className="runtime-kpis" aria-label="BIOS 依赖摘要">
      <article><small>当前范围</small><strong>{response.summary.totalCount}</strong><p>{scope === "REQUIRED_BY_LIBRARY" ? "游戏库实际引用的依赖" : "全部已支持核心的目录"}</p></article>
      <article className={response.summary.blockingCount ? "has-danger" : ""}><small>缺失 / 阻断</small><strong>{response.summary.blockingCount}</strong><p>必需文件缺失会阻断相关游戏</p></article>
      <article className={response.summary.warningCount ? "has-warning" : ""}><small>需要核对</small><strong>{response.summary.warningCount}</strong><p>哈希不同仍可启动，建议替换</p></article>
      <article className="has-success"><small>已就绪</small><strong>{response.summary.readyCount}</strong><p>已经安装并通过当前校验</p></article>
    </section>

    <section className="runtime-toolbar panel" aria-label="筛选 BIOS 文件">
      <label className="runtime-search"><span>搜索文件或运行方式</span><span className="search"><AppIcon name="search" /><input type="search" aria-label="搜索 BIOS 文件" placeholder="例如 gba_bios.bin 或 mGBA" value={filters.query} onChange={(event) => patchFilters({ query: event.target.value })} /></span></label>
      <label><span>运行方式</span><select className="select" aria-label="运行方式" value={filters.coreId} onChange={(event) => patchFilters({ coreId: event.target.value })}><option value="">全部运行方式</option>{cores.map(([id, name]) => <option value={id} key={id}>{name}</option>)}</select></label>
      <label><span>文件状态</span><select className="select" aria-label="文件状态" value={filters.status} onChange={(event) => patchFilters({ status: event.target.value })}><option value="">所有状态</option><option value="MISSING">缺少文件</option><option value="MISSING_ENTRY">归档不完整</option><option value="HASH_WARNING">校验值不一致</option><option value="MATCHED">已安装并匹配</option><option value="OPTIONAL_MISSING">可选文件未安装</option></select></label>
    </section>

    <div className="runtime-chips" aria-label="BIOS 快速筛选">{quickFilters.map(([value, label, count]) => <button type="button" className={filters.quick === value ? "is-active" : ""} aria-pressed={filters.quick === value} onClick={() => patchFilters({ quick: value })} key={value}>{label} {count}</button>)}</div>

    <BIOSResults
      announcement={announcement}
      attention={attention}
      busy={busy}
      firstError={firstError}
      firstLoading={firstLoading}
      inputs={inputs}
      loadMore={loadMore}
      nextError={nextError}
      nextLoading={nextLoading}
      onClear={() => setFilters({ query: "", coreId: "", status: "", quick: "ALL" })}
      onInstall={(requirement, file) => void install(requirement, file)}
      onInspect={(requirement) => void inspectArchive(requirement)}
      onReload={reloadFirst}
      ready={ready}
      response={response}
      scope={scope}
      sentinel={sentinel}
    />
    <p className="runtime-server-import-link"><Link href="/admin/imports/server?action=bios">从服务器目录批量导入 BIOS</Link></p>
    <ArchiveInspectionDialog archiveDialog={archiveDialog} onClose={() => setArchiveDialog(null)} />
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
    <ul className="bios-entry-field-head" aria-label={`${title}字段表头`}><li>name</li><li>size</li><li>crc</li></ul>
    <ul className="bios-entry-list" aria-label={ariaLabel}>{entries.map((entry, index) => <li className={`bios-entry-card is-${entry.status.toLowerCase()}`} title={entryStatusLabels[entry.status]} key={`${entry.facts.name}-${index}`}>
      <span className="sr-only">状态：{entryStatusLabels[entry.status]}</span>
      <ArchiveFacts facts={entry.facts} />
    </li>)}</ul>
  </section>;
}

function ArchiveFacts({ facts }: { facts: ArchiveEntryFacts }) {
  return <ul className="bios-entry-facts" aria-label={`${facts.name} 文件信息`}>
    <li><code>{facts.name}</code></li>
    <li><strong>{facts.sizeBytes.toLocaleString("zh-CN")} bytes</strong></li>
    <li><code>{facts.crc32 || "—"}</code></li>
  </ul>;
}
