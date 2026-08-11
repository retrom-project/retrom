"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast } from "@/components/flash-toast";
import { StatusBadge } from "@/components/ui";
import { api, writeHeaders } from "@/lib/api/client";
import type { components } from "@/lib/api/generated/schema";
import { newUuid } from "@/lib/crypto";
import { responseError } from "@/lib/upload";

export type ServerImportRoot = components["schemas"]["ServerImportRoot"];
export type ServerImportSummary = components["schemas"]["ServerImportSummary"];
export type ServerImportList = components["schemas"]["ServerImportList"];
export type ServerImportDetail = components["schemas"]["ServerImportDetail"];
type Directory = components["schemas"]["ServerImportDirectory"];
type Candidate = components["schemas"]["ServerBIOSImportCandidate"];
type ImportItem = components["schemas"]["ServerBIOSImportItem"];

const stateLabels: Record<ServerImportSummary["state"], string> = {
  QUEUED: "等待开始", RUNNING: "正在导入", COMPLETED: "已完成", PARTIAL_FAILURE: "部分失败",
  CANCEL_REQUESTED: "正在取消", CANCELLED: "已取消", FAILED: "任务失败",
};
const phaseLabels: Record<NonNullable<ServerImportSummary["phase"]>, string> = {
  PREPARING_ROOT: "准备服务器位置", DISCOVERING: "遍历目录", HASHING: "计算校验值",
  VALIDATING_ARCHIVES: "检查 ZIP", DISCOVERY_COMPLETED: "候选发现完成", RANKING: "候选排序",
  INSTALLING: "逐项安装", QUEUEING_REVALIDATION: "安排后续验证",
};
const itemLabels: Record<ImportItem["state"], string> = {
  PENDING: "等待处理", EVALUATING: "正在评估", IMPORTED_MATCHED: "已导入·匹配", IMPORTED_WARNING: "已导入·警告",
  IMPORTED_MISSING_ENTRY: "已导入·缺少条目", NOT_FOUND: "未找到", SKIPPED_EXISTING: "保留已有 BIOS",
  SKIPPED_NOT_BETTER: "候选不更优", ALREADY_SAME_BYTES: "内容相同", SOURCE_CHANGED: "源文件已变化",
  CATALOG_CHANGED: "目录版本已变化", READ_FAILED: "读取失败", COMMIT_FAILED: "提交失败", CANCELLED: "已取消",
};

function stateTone(state: ServerImportSummary["state"]): "good" | "warn" | "bad" | "info" {
  if (state === "COMPLETED") return "good";
  if (state === "FAILED" || state === "PARTIAL_FAILURE") return "bad";
  if (state === "CANCELLED" || state === "CANCEL_REQUESTED") return "warn";
  return "info";
}

function errorMessage(response: Response, fallback: string) {
  return responseError(response, fallback);
}

export function ServerImportManager({ initialRoots, initialImports, initialOpen = false, initialCatalogSummary }: {
  initialRoots: ServerImportRoot[];
  initialImports: ServerImportList;
  initialOpen?: boolean;
  initialCatalogSummary?: { totalCount: number; attentionCount: number };
}) {
  const router = useRouter();
  const drawer = useRef<HTMLElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const roots = initialRoots;
  const [imports, setImports] = useState(initialImports.items);
  const [historyCursor, setHistoryCursor] = useState(initialImports.nextCursor);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(initialOpen);
  const [rootId, setRootId] = useState(initialRoots.find((root) => root.status === "AVAILABLE")?.id ?? "");
  const [path, setPath] = useState("");
  const [directories, setDirectories] = useState<Directory[]>([]);
  const [directoryNextCursor, setDirectoryNextCursor] = useState<string | null>(null);
  const [directoryLoading, setDirectoryLoading] = useState(false);
  const [replaceIfBetter, setReplaceIfBetter] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!drawerOpen) return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    closeButton.current?.focus();
    return () => previous?.focus();
  }, [drawerOpen]);

  useEffect(() => {
    if (!drawerOpen || !rootId) return;
    const controller = new AbortController();
    const load = async () => {
      setDirectoryLoading(true); setError("");
      try {
        const { data, response } = await api.GET("/api/v1/admin/server-import-roots/{rootId}/directories", {
          params: { path: { rootId }, query: { path, limit: 100 } }, signal: controller.signal,
        });
        if (!data) throw new Error(await errorMessage(response, "服务器目录读取失败"));
        setDirectories(data.items);
        setDirectoryNextCursor(data.nextCursor);
      } catch (caught) {
        if (!(caught instanceof DOMException && caught.name === "AbortError")) setError(caught instanceof Error ? caught.message : "服务器目录读取失败");
      } finally {
        setDirectoryLoading(false);
      }
    };
    void load();
    return () => controller.abort();
  }, [drawerOpen, path, rootId]);

  async function loadMoreDirectories() {
    if (!directoryNextCursor || directoryLoading) return;
    setDirectoryLoading(true); setError("");
    try {
      const { data, response } = await api.GET("/api/v1/admin/server-import-roots/{rootId}/directories", {
        params: { path: { rootId }, query: { path, cursor: directoryNextCursor, limit: 100 } },
      });
      if (!data) throw new Error(await errorMessage(response, "服务器目录读取失败"));
      setDirectories((current) => {
        const known = new Set(current.map((directory) => directory.relativePath));
        return [...current, ...data.items.filter((directory) => !known.has(directory.relativePath))];
      });
      setDirectoryNextCursor(data.nextCursor);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "服务器目录读取失败");
    } finally {
      setDirectoryLoading(false);
    }
  }

  useEffect(() => {
    const active = imports.some((item) => ["QUEUED", "RUNNING", "CANCEL_REQUESTED"].includes(item.state));
    if (!active) return;
    const timer = window.setInterval(() => {
      void api.GET("/api/v1/admin/server-imports", { params: { query: { kind: "BIOS_DIRECTORY", limit: 20 } } }).then(({ data }) => {
        if (data) setImports(data.items);
      });
    }, 3000);
    return () => window.clearInterval(timer);
  }, [imports]);

  async function loadMoreHistory() {
    if (!historyCursor || historyLoading) return;
    setHistoryLoading(true); setError("");
    try {
      const { data, response } = await api.GET("/api/v1/admin/server-imports", {
        params: { query: { kind: "BIOS_DIRECTORY", cursor: historyCursor, limit: 20 } },
      });
      if (!data) throw new Error(await errorMessage(response, "导入历史读取失败"));
      setImports((current) => {
        const known = new Set(current.map((item) => item.id));
        return [...current, ...data.items.filter((item) => !known.has(item.id))];
      });
      setHistoryCursor(data.nextCursor);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "导入历史读取失败");
    } finally {
      setHistoryLoading(false);
    }
  }

  const selectedRoot = roots.find((root) => root.id === rootId);
  const breadcrumbs = useMemo(() => path ? path.split("/") : [], [path]);

  function closeDrawer() {
    if (!busy) setDrawerOpen(false);
  }

  async function createImport() {
    if (!rootId || selectedRoot?.status !== "AVAILABLE") return;
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/server-imports", {
        params: { header: { ...writeHeaders(), "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } },
        body: { kind: "BIOS_DIRECTORY", rootId, sourceRelativePath: path, replaceIfBetter },
      });
      if (!data) throw new Error(await errorMessage(response, "服务器导入创建失败"));
      setDrawerOpen(false);
      router.push(`/admin/imports/server/${data.id}`);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "服务器导入创建失败");
    } finally {
      setBusy(false);
    }
  }

  const catalogSummary = initialCatalogSummary ?? { totalCount: 0, attentionCount: 0 };
  return <div className="server-import-page">
    <section className="server-import-hero panel">
      <div><span className="eyebrow">BIOS DIRECTORY</span><h2>扫描并导入 BIOS</h2><p>从部署者允许的只读位置检查完整 BIOS 目录。完整发现结束后才逐项安装；默认不会替换已有 BIOS。</p><dl className="server-import-capability-stats"><div><dt>完整目录</dt><dd>{catalogSummary.totalCount}</dd></div><div><dt>需要处理</dt><dd>{catalogSummary.attentionCount}</dd></div><div><dt>最近任务</dt><dd>{imports[0] ? stateLabels[imports[0].state] : "暂无"}</dd></div></dl></div>
      <button type="button" className="button" disabled={!roots.some((root) => root.status === "AVAILABLE") || imports.some((item) => ["QUEUED", "RUNNING", "CANCEL_REQUESTED"].includes(item.state))} onClick={() => setDrawerOpen(true)}>选择目录并开始</button>
    </section>

    <section className="server-import-roots" aria-label="服务器位置">
      {roots.length ? roots.map((root) => <article className="panel" key={root.id}><div><span className="server-root-mark" aria-hidden="true">SRV</span><div><h3>{root.label}</h3><p>{root.id}</p></div></div><StatusBadge tone={root.status === "AVAILABLE" ? "good" : "bad"}>{root.status === "AVAILABLE" ? "可用" : "不可用"}</StatusBadge></article>) : <article className="runtime-inline-empty compact"><h2>尚未配置服务器位置</h2><p>请由部署管理员设置 RETROM_SERVER_IMPORT_ROOTS 后重启服务。</p></article>}
    </section>

    <section className="server-import-history">
      <div className="runtime-section-heading"><div><h2>导入历史</h2><p>任务离开页面后仍会继续；详情页会恢复实时进度。</p></div><Link href="/admin/bios">查看 BIOS 文件 →</Link></div>
      {imports.length ? <><div className="server-import-task-list">{imports.map((item) => <Link href={`/admin/imports/server/${item.id}`} className="server-import-task panel" key={item.id}><div><StatusBadge tone={stateTone(item.state)}>{stateLabels[item.state]}</StatusBadge><h3>{item.root.label}{item.sourceRelativePath ? ` / ${item.sourceRelativePath}` : " / 根目录"}</h3><p>{item.phase ? phaseLabels[item.phase] : "等待任务进度"} · {new Date(item.createdAtMs).toLocaleString("zh-CN")}</p></div><dl><div><dt>候选</dt><dd>{item.counts.candidates}</dd></div><div><dt>已评估</dt><dd>{item.counts.evaluatedItems}/{item.counts.catalogItems}</dd></div><div><dt>已导入</dt><dd>{item.counts.imported}</dd></div><div><dt>失败</dt><dd>{item.counts.failed}</dd></div></dl><span aria-hidden="true">→</span></Link>)}</div>{historyCursor ? <button type="button" className="button secondary server-import-history-more" disabled={historyLoading} onClick={() => void loadMoreHistory()}>{historyLoading ? "正在读取…" : "查看全部历史"}</button> : null}</> : <div className="runtime-inline-empty"><h2>还没有服务器导入任务</h2><p>选择一个允许的位置和目录后，Retrom 会异步发现并验证 BIOS 候选。</p></div>}
    </section>

    {drawerOpen ? <><button type="button" className="runtime-drawer-backdrop" aria-label="关闭服务器导入" disabled={busy} onClick={closeDrawer} /><aside ref={drawer} className="runtime-drawer server-import-drawer" role="dialog" aria-modal="true" aria-labelledby="server-import-drawer-title" onKeyDown={(event) => {
      if (event.key === "Escape") { event.preventDefault(); closeDrawer(); }
      if (event.key !== "Tab") return;
      const focusable = Array.from(drawer.current?.querySelectorAll<HTMLElement>("button:not(:disabled),input:not(:disabled),select:not(:disabled),a[href],[tabindex]:not([tabindex='-1'])") ?? []);
      if (!focusable.length) return;
      const first = focusable[0]; const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    }}><header><div><StatusBadge tone="info">服务器导入</StatusBadge><h2 id="server-import-drawer-title">选择 BIOS 所在目录</h2><p>只能浏览允许位置内的目录，不会显示宿主机绝对路径。</p></div><button ref={closeButton} type="button" className="runtime-drawer-close" aria-label="关闭" disabled={busy} onClick={closeDrawer}><AppIcon name="x" /></button></header><div className="runtime-drawer-body">
      <fieldset className="server-root-options"><legend>服务器位置</legend>{roots.map((root) => <label key={root.id}><input type="radio" name="server-import-root" value={root.id} checked={rootId === root.id} disabled={busy || root.status !== "AVAILABLE"} onChange={(event) => { setRootId(event.target.value); setPath(""); }} /><span><strong>{root.label}</strong><small>{root.status === "AVAILABLE" ? "可用" : "不可用"}</small></span></label>)}</fieldset>
      <div className="server-directory-browser"><nav aria-label="当前目录"><button type="button" onClick={() => setPath("")} disabled={!path || busy}>根目录</button>{breadcrumbs.map((part, index) => <button type="button" key={`${part}-${index}`} disabled={index === breadcrumbs.length - 1 || busy} onClick={() => setPath(breadcrumbs.slice(0, index + 1).join("/"))}>/ {part}</button>)}</nav>
        {!directories.length && directoryLoading ? <p role="status"><span className="button-spinner" />正在读取子目录…</p> : directories.length ? <><ul>{directories.map((directory) => <li key={directory.relativePath}><button type="button" disabled={busy} onClick={() => setPath(directory.relativePath)}><AppIcon name="folder" /><span>{directory.name}</span><span aria-hidden="true">→</span></button></li>)}</ul>{directoryNextCursor ? <button type="button" className="button secondary compact server-directory-more" disabled={directoryLoading || busy} onClick={() => void loadMoreDirectories()}>{directoryLoading ? "正在读取…" : "加载更多目录"}</button> : null}</> : <p>当前目录没有可进入的子目录。</p>}
      </div>
      <label className="server-import-overwrite"><input type="checkbox" checked={replaceIfBetter} disabled={busy} onChange={(event) => setReplaceIfBetter(event.target.checked)} /><span><strong>允许使用更优候选替换已有 BIOS</strong><small>只有按同一目录证据严格更优时才替换；同分、更差或证据不完整都会保留当前安装。</small></span></label>
      <div className="server-import-selection-summary" aria-live="polite"><strong>{selectedRoot?.label ?? "未选择服务器位置"} / {path}</strong><span>将检查当前系统完整 BIOS 目录，共 {catalogSummary.totalCount} 项；包含可选和按需 BIOS，不只检查已导入游戏。</span></div>
    </div><footer><button type="button" className="button secondary" disabled={busy} onClick={closeDrawer}>取消</button><button type="button" className="button" disabled={busy || !rootId || selectedRoot?.status !== "AVAILABLE"} onClick={() => void createImport()}>{busy ? "正在创建…" : "开始异步导入"}</button></footer></aside></> : null}
    <Toast toast={error ? { message: error, tone: "bad" } : null} onDismiss={() => setError("")} />
  </div>;
}

type DetailFilters = { query: string; outcome: string; matchMethod: string };

export function ServerImportDetailManager({ initialDetail, initialFilters = { query: "", outcome: "", matchMethod: "" } }: { initialDetail: ServerImportDetail; initialFilters?: DetailFilters }) {
  const [detail, setDetail] = useState(initialDetail);
  const [connection, setConnection] = useState("正在连接实时进度");
  const [error, setError] = useState("");
  const [cancelOpen, setCancelOpen] = useState(false);
  const [cancelReason, setCancelReason] = useState("管理员停止服务器 BIOS 导入");
  const [busy, setBusy] = useState(false);
  const [candidateItem, setCandidateItem] = useState<ImportItem | null>(null);
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const [candidateNextCursor, setCandidateNextCursor] = useState<string | null>(null);
  const [candidateLoading, setCandidateLoading] = useState(false);
  const [query, setQuery] = useState(initialFilters.query);
  const [outcome, setOutcome] = useState(initialFilters.outcome);
  const [matchMethod, setMatchMethod] = useState(initialFilters.matchMethod);
  const [appliedFilters, setAppliedFilters] = useState(initialFilters);
  const [itemLoading, setItemLoading] = useState(false);

  const requestDetail = useCallback(async (filters: typeof appliedFilters, cursor?: string, append = false) => {
    const { data, response } = await api.GET("/api/v1/admin/server-imports/{serverImportId}", {
      params: { path: { serverImportId: initialDetail.summary.id }, query: {
        q: filters.query || undefined,
        outcome: filters.outcome || undefined,
        matchMethod: filters.matchMethod || undefined,
        cursor,
        limit: 50,
      } },
    });
    if (!data) throw new Error(await errorMessage(response, "导入详情读取失败"));
    setDetail((current) => {
      if (!append) return data;
      const known = new Set(current.items.map((item) => item.requirementId));
      return { ...data, items: [...current.items, ...data.items.filter((item) => !known.has(item.requirementId))] };
    });
  }, [initialDetail.summary.id]);

  const refresh = useCallback(async () => requestDetail(appliedFilters), [appliedFilters, requestDetail]);

  useEffect(() => {
    const values = new URLSearchParams(window.location.search);
    const updates = { q: appliedFilters.query, outcome: appliedFilters.outcome, matchMethod: appliedFilters.matchMethod };
    for (const [name, value] of Object.entries(updates)) {
      if (value) values.set(name, value); else values.delete(name);
    }
    const encoded = values.toString();
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${encoded ? `?${encoded}` : ""}`);
  }, [appliedFilters]);

  async function applyItemFilters() {
    const filters = { query: query.trim(), outcome, matchMethod };
    setAppliedFilters(filters); setItemLoading(true); setError("");
    try { await requestDetail(filters); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "导入详情读取失败"); }
    finally { setItemLoading(false); }
  }

  async function loadMoreItems() {
    if (!detail.nextCursor || itemLoading) return;
    setItemLoading(true); setError("");
    try { await requestDetail(appliedFilters, detail.nextCursor, true); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "导入详情读取失败"); }
    finally { setItemLoading(false); }
  }

  useEffect(() => {
    if (!["QUEUED", "RUNNING", "CANCEL_REQUESTED"].includes(detail.summary.state)) return;
    const source = new EventSource(`/api/v1/admin/jobs/${encodeURIComponent(detail.summary.jobId)}/events`, { withCredentials: true });
    const update = () => { setConnection("实时进度已连接"); void refresh().catch((caught: unknown) => setError(caught instanceof Error ? caught.message : "进度刷新失败")); };
    for (const name of ["snapshot", "progress", "started", "succeeded", "failed", "cancelled"]) source.addEventListener(name, update);
    source.onerror = () => setConnection("实时连接中断，正在自动重连");
    const fallback = window.setInterval(update, 5000);
    return () => { source.close(); window.clearInterval(fallback); };
  }, [detail.summary.id, detail.summary.jobId, detail.summary.state, refresh]);

  async function cancel() {
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/server-imports/{serverImportId}/cancel", {
        params: { path: { serverImportId: detail.summary.id }, header: { ...writeHeaders(), "If-Match": `"v${detail.summary.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } },
        body: { reason: cancelReason },
      });
      if (!data) throw new Error(await errorMessage(response, "取消任务失败"));
      setDetail((current) => ({ ...current, summary: data })); setCancelOpen(false);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "取消任务失败"); }
    finally { setBusy(false); }
  }

  async function retry() {
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/server-imports/{serverImportId}/retry", {
        params: { path: { serverImportId: detail.summary.id }, header: { ...writeHeaders(), "If-Match": `"v${detail.summary.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } }, body: {},
      });
      if (!data) throw new Error(await errorMessage(response, "重试任务失败"));
      setDetail((current) => ({ ...current, summary: data }));
    } catch (caught) { setError(caught instanceof Error ? caught.message : "重试任务失败"); }
    finally { setBusy(false); }
  }

  async function inspectCandidates(item: ImportItem) {
    setCandidateItem(item); setCandidateLoading(true); setCandidates([]); setError("");
    try {
      const { data, response } = await api.GET("/api/v1/admin/server-imports/{serverImportId}/bios-items/{requirementId}/candidates", { params: { path: { serverImportId: detail.summary.id, requirementId: item.requirementId }, query: { limit: 50 } } });
      if (!data) throw new Error(await errorMessage(response, "候选详情读取失败"));
      setCandidates(data.items);
      setCandidateNextCursor(data.nextCursor);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "候选详情读取失败"); }
    finally { setCandidateLoading(false); }
  }

  async function loadMoreCandidates() {
    if (!candidateItem || !candidateNextCursor || candidateLoading) return;
    setCandidateLoading(true); setError("");
    try {
      const { data, response } = await api.GET("/api/v1/admin/server-imports/{serverImportId}/bios-items/{requirementId}/candidates", { params: { path: { serverImportId: detail.summary.id, requirementId: candidateItem.requirementId }, query: { cursor: candidateNextCursor, limit: 50 } } });
      if (!data) throw new Error(await errorMessage(response, "候选详情读取失败"));
      setCandidates((current) => {
        const known = new Set(current.map((candidate) => candidate.id));
        return [...current, ...data.items.filter((candidate) => !known.has(candidate.id))];
      });
      setCandidateNextCursor(data.nextCursor);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "候选详情读取失败"); }
    finally { setCandidateLoading(false); }
  }

  const summary = detail.summary;
  const connectionLabel = ["QUEUED", "RUNNING", "CANCEL_REQUESTED"].includes(summary.state) ? connection : "任务已结束";
  const canCancel = summary.state === "QUEUED" || summary.state === "RUNNING";
  const canRetry = summary.state === "FAILED" && ["SERVER_IMPORT_ROOT_UNAVAILABLE", "INTERNAL_ERROR"].includes(summary.lastErrorCode ?? "");
  return <div className="server-import-detail-page">
    <section className="server-import-detail-head panel"><div><StatusBadge tone={stateTone(summary.state)}>{stateLabels[summary.state]}</StatusBadge><h2>{summary.root.label}{summary.sourceRelativePath ? ` / ${summary.sourceRelativePath}` : " / 根目录"}</h2><p>{summary.phase ? phaseLabels[summary.phase] : "任务尚未进入处理阶段"} · <span aria-live="polite">{connectionLabel}</span></p></div><div>{canCancel ? <button type="button" className="button secondary" disabled={busy} onClick={() => setCancelOpen(true)}>取消任务</button> : null}{canRetry ? <button type="button" className="button" disabled={busy} onClick={() => void retry()}>重试原任务</button> : null}<Link className="button secondary" href="/admin/imports/server?action=bios">新建导入</Link></div></section>
    <section className="runtime-kpis" aria-label="服务器导入摘要"><article><small>目录项</small><strong>{summary.counts.catalogItems}</strong><p>创建任务时冻结的完整 BIOS 目录</p></article><article><small>候选</small><strong>{summary.counts.candidates}</strong><p>通过文件名或可靠 hash 建立的候选</p></article><article className="has-success"><small>已导入</small><strong>{summary.counts.imported}</strong><p>{summary.counts.matched} 项完整匹配</p></article><article className={summary.counts.failed ? "has-danger" : ""}><small>失败 / 冲突</small><strong>{summary.counts.failed + summary.counts.conflicts}</strong><p>保留逐项证据，不回滚已成功安装</p></article></section>
    {summary.lastErrorCode ? <p className="server-import-error panel"><strong>{summary.lastErrorCode}</strong><span>服务器源位置不属于备份；若目录或 catalog 已变化，请新建任务。</span></p> : null}
    <section className="server-import-results"><div className="runtime-section-heading"><div><h2>BIOS 处理结果</h2><p>候选和安装始终按 Requirement / CoreArtifact 隔离。</p></div><span>{detail.items.length} / {summary.counts.catalogItems} 项</span></div>
      <form className="server-import-result-filters panel" aria-label="筛选导入结果" onSubmit={(event) => { event.preventDefault(); void applyItemFilters(); }}><label><span>搜索 BIOS 或核心</span><input type="search" value={query} placeholder="例如 gba_bios.bin" onChange={(event) => setQuery(event.target.value)} /></label><label><span>结果</span><select value={outcome} onChange={(event) => setOutcome(event.target.value)}><option value="">全部结果</option>{Object.entries(itemLabels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select></label><label><span>匹配方式</span><select value={matchMethod} onChange={(event) => setMatchMethod(event.target.value)}><option value="">全部方式</option><option value="EXACT_HASH">精确 Hash</option><option value="EXPECTED_SIZE_FALLBACK">期望大小</option><option value="LARGEST_SIZE_FALLBACK">最大文件</option><option value="DAT_ENTRY_MATCH">DAT 完整匹配</option><option value="DAT_ENTRY_WARNING">DAT 警告</option><option value="DAT_PARTIAL_FALLBACK">DAT 部分匹配</option></select></label><button type="submit" className="button secondary compact" disabled={itemLoading}>{itemLoading ? "正在筛选…" : "应用筛选"}</button></form>
      <div className="server-import-result-table" role="table" aria-label="BIOS 导入结果">{detail.items.map((item) => <article role="row" key={item.requirementId}><div role="cell"><strong>{item.logicalName}</strong><small>{item.coreName} · {item.coreArtifactId} · {item.requirementMode}</small>{item.selectedRelativePath ? <small title={item.selectedRelativePath}>候选：{item.selectedRelativePath}</small> : null}</div><div role="cell"><StatusBadge tone={item.state.startsWith("IMPORTED") ? "good" : ["SOURCE_CHANGED", "CATALOG_CHANGED", "READ_FAILED", "COMMIT_FAILED"].includes(item.state) ? "bad" : "warn"}>{itemLabels[item.state]}</StatusBadge><small>{item.replaced ? "已替换现有安装" : "未替换现有安装"}</small></div><div role="cell"><strong>{item.matchMethod ?? "—"}</strong><small>{item.outcomeCode ?? "尚无结果码"}</small><small>{item.previousInstallationStatus ?? "无旧安装"} → {item.newInstallationStatus ?? "无新安装"}</small></div><div role="cell"><button type="button" className="button secondary compact" disabled={item.candidateCount === 0} onClick={() => void inspectCandidates(item)}>查看候选（{item.candidateCount}）</button></div></article>)}</div>{detail.nextCursor ? <div className="server-import-result-more"><button type="button" className="button secondary" disabled={itemLoading} onClick={() => void loadMoreItems()}>{itemLoading ? "正在加载…" : "加载更多结果"}</button></div> : null}
    </section>
    <ConfirmDialog open={cancelOpen} title="取消这次服务器导入？" description="已经导入的 BIOS 不会回滚，尚未处理的项目将停止。" confirmLabel="确认取消" tone="danger" busy={busy} onCancel={() => setCancelOpen(false)} onConfirm={() => void cancel()}><label className="server-import-cancel-reason"><span>取消原因</span><input value={cancelReason} maxLength={500} onChange={(event) => setCancelReason(event.target.value)} /></label></ConfirmDialog>
    <ConfirmDialog wide open={candidateItem !== null} title={`${candidateItem?.logicalName ?? "BIOS"} 候选排序`} description="排序先比较可用性和内容证据，再使用大小、hash 与相对路径作为确定性 tie-break。" confirmLabel="关闭" hideCancel onCancel={() => setCandidateItem(null)} onConfirm={() => setCandidateItem(null)}>{candidateLoading && !candidates.length ? <p role="status">正在读取候选…</p> : candidates.length ? <><div className="server-candidate-list">{candidates.map((candidate) => { const evidence = candidate.evaluationDetails ?? {}; return <article key={candidate.id}><header><StatusBadge tone={candidate.state === "SELECTED" ? "good" : candidate.state === "ELIGIBLE" ? "info" : "warn"}>{candidate.state}</StatusBadge><strong>#{candidate.rankOrdinal ?? "—"}</strong></header><h3>{candidate.relativePath}</h3><p>{candidate.associationKind} · {candidate.sizeBytes.toLocaleString("zh-CN")} bytes · {candidate.notSelectedReason ?? "当前最优候选"}</p>{"matchedCount" in evidence ? <p>DAT：匹配 {String(evidence.matchedCount ?? 0)} · 别名 {String(evidence.aliasedCount ?? 0)} · 不一致 {String(evidence.mismatchedCount ?? 0)} · 缺失 {String(evidence.missingCount ?? 0)} · 额外 {String(evidence.extraCount ?? 0)}</p> : <p>{evidence.exactHash ? "完整 hash 匹配" : evidence.expectedSizeMatched ? "期望大小推测" : "按大小推测"}</p>}<code>SHA-256 {candidate.sha256 ?? "未读取"}</code></article>; })}</div>{candidateNextCursor ? <button type="button" className="button secondary server-candidate-more" disabled={candidateLoading} onClick={() => void loadMoreCandidates()}>{candidateLoading ? "正在加载…" : "加载更多候选"}</button> : null}</> : <p>没有可展示的候选。</p>}</ConfirmDialog>
    <Toast toast={error ? { message: error, tone: "bad" } : null} onDismiss={() => setError("")} />
  </div>;
}
