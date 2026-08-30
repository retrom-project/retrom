"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { StatusBadge } from "@/components/ui";
import { formatTime, type ListResponse } from "@/lib/backend";
import { statusTone } from "@/lib/status";
import {
  filterImportTasks,
  importProviderLabels,
  importStageIndex,
  importStateLabels,
  importTaskIssueCount,
  importTaskIssueSummary,
  importTaskPhase,
  importTaskProgress,
  importTaskSummary,
  type ImportDetail,
  type ImportListItem,
  type ImportTaskFilters,
} from "./import-workflow";

const stages = ["上传", "识别", "运行检查", "游戏信息", "人工审核", "发布"];

type DetailState = { status: "loading" | "ready" | "error"; value?: ImportDetail };

const rejectionLabels: Record<string, string> = {
  ARCHIVE_UNSAFE: "归档内容或文件名未通过安全检查",
  ARCHIVE_LIMIT_EXCEEDED: "归档超过数量、大小或压缩比限制",
  ARCHIVE_ENCRYPTED_UNSUPPORTED: "不支持加密归档",
  ARCHIVE_VOLUME_UNSUPPORTED: "不支持分卷归档",
  ARCHIVE_RESOURCE_LIMIT: "归档处理超过资源限制",
  ARCHIVE_SANDBOX_UNAVAILABLE: "归档安全处理环境不可用",
  NESTED_ARCHIVE_UNSUPPORTED: "不支持归档内再次嵌套归档",
  ARCHIVE_METHOD_UNSUPPORTED: "不支持该归档压缩方式",
  ARCHIVE_CASEFOLD_COLLISION: "归档内存在仅大小写不同的冲突路径",
  UNSUPPORTED_CONTENT_FORMAT: "该目录不支持这种文件格式",
  NO_SUPPORTED_CONTENT: "归档内没有该平台支持的游戏文件",
  AMBIGUOUS_PRIMARY_CONTENT: "归档内有多个候选游戏文件",
  ARCADE_MACHINE_NOT_FOUND: "文件名无法在当前核心启用的街机数据目录中匹配到 machine",
  ARCADE_UNUSED_DEPENDENCY_ARCHIVE: "这是未被同批游戏引用的街机依赖包，请改由 BIOS 文件页面安装",
  MULTI_DISC_CHD_INVALID: "光盘文件不是有效的 CHD",
  MULTI_DISC_LIMIT_EXCEEDED: "多盘目录超过光盘数量或总大小限制",
  MULTI_DISC_PLAYLIST_INVALID: "M3U 播放列表内容无效",
  MULTI_DISC_REFERENCE_UNSAFE: "M3U 包含不安全或不受支持的引用",
};

const rejectionDetails: Record<string, string> = {
  ARCHIVE_UNSAFE: "归档包含不安全路径、符号链接或其他可能越出解包目录的内容。请检查归档结构后重新配置。",
  ARCHIVE_LIMIT_EXCEEDED: "归档的文件数量、展开后总大小或压缩比超过安全上限，系统不会继续展开。",
  ARCHIVE_ENCRYPTED_UNSUPPORTED: "归档已加密，服务器无法在不接收密码的情况下安全检查其内容。",
  ARCHIVE_VOLUME_UNSUPPORTED: "检测到分卷归档；请先在本地合并为单个受支持归档。",
  ARCHIVE_RESOURCE_LIMIT: "归档扫描消耗的时间或资源超过限制，未进入后续识别。",
  ARCHIVE_SANDBOX_UNAVAILABLE: "安全归档处理环境暂时不可用，可以稍后重新配置重试。",
  NESTED_ARCHIVE_UNSUPPORTED: "归档内部仍包含归档文件，系统不会递归展开。",
  ARCHIVE_METHOD_UNSUPPORTED: "归档使用了当前 7z/ZIP 读取器不支持的压缩方法。",
  ARCHIVE_CASEFOLD_COLLISION: "归档内存在仅大小写不同的同名路径，跨平台展开结果不确定。",
  UNSUPPORTED_CONTENT_FORMAT: "目标游戏目录不接收该文件格式，请重新选择正确目录。",
  NO_SUPPORTED_CONTENT: "归档中没有找到目标游戏目录支持的可运行内容。",
  AMBIGUOUS_PRIMARY_CONTENT: "归档中存在多个可作为主游戏的文件，系统无法替用户决定。",
  ARCADE_MACHINE_NOT_FOUND: "归档文件名去掉 .zip 后未命中当前核心启用 DAT 的 machine 名；可能选错了街机目录或 DAT 不包含该 ROMset。",
  ARCADE_UNUSED_DEPENDENCY_ARCHIVE: "该街机 BIOS/父级包没有被本批次任何游戏引用，因此没有单独创建游戏。",
  MULTI_DISC_CHD_INVALID: "至少一个被引用文件缺少 CHD 文件头；请重新选择包含完整有效 CHD 的目录。",
  MULTI_DISC_LIMIT_EXCEEDED: "该目录超过当前核心声明的 2–8 张光盘或 1 GiB 总大小限制。",
  MULTI_DISC_PLAYLIST_INVALID: "播放列表必须是 UTF-8 文本，并按顺序包含 2–8 个安全 CHD 文件名。",
  MULTI_DISC_REFERENCE_UNSAFE: "播放列表只能引用同目录的 CHD basename，不能包含路径、URI 或边界空白。",
};

const activeImportStates = new Set(["QUEUED", "RUNNING", "CANCEL_REQUESTED"]);

function refreshListItem(item: ImportListItem, detail: ImportDetail): ImportListItem {
  return {
    ...item,
    state: detail.state,
    platformInstanceName: detail.targetPlatformInstance.name,
    metadataProvider: detail.metadataProvider,
    contentMode: detail.configSnapshot?.contentMode ?? item.contentMode,
    totalItemCount: detail.counts.total,
    reviewPendingItemCount: detail.counts.reviewPending,
    failedItemCount: detail.counts.failed,
    rejectedFileCount: detail.counts.rejectedFiles,
    unresolvedRejectedFileCount: detail.counts.unresolvedRejectedFiles,
    alreadyImportedItemCount: detail.counts.alreadyImportedItems,
    alreadyImportedFileCount: detail.counts.alreadyImportedFiles,
    lastErrorCode: detail.errorCode ?? null,
    version: detail.version,
    createdAtMs: detail.createdAtMs,
    updatedAtMs: detail.updatedAtMs,
  };
}

function MultiDiscTaskDetail({ detail }: { detail?: DetailState }) {
  if (!detail || detail.status === "loading") {return <p className="import-task-detail-message">正在读取多盘目录明细…</p>;}
  if (detail.status === "error") {return <p className="import-task-detail-message bad">多盘目录明细读取失败，请稍后重试。</p>;}
  const summaries = detail.value?.itemSummaries ?? [];
  if (!summaries.length) {return <p className="import-task-detail-message">这个任务没有可显示的多盘目录。</p>;}
  const stateLabel = (state: string, missingDiscCount: number) => {
    if (missingDiscCount) {return `待审核 · 缺 ${missingDiscCount} 张`;}
    if (state === "PUBLISHED") {return "已发布 · 目录完整";}
    if (state === "DISCARDED") {return "已丢弃 · 目录完整";}
    if (state === "FAILED_RETRYABLE" || state === "FAILED_FINAL") {return "处理失败 · 目录完整";}
    return "待审核 · 目录完整";
  };
  return <div className="import-task-multidisc" aria-label="多盘目录明细">
    {summaries.map((summary) => <article key={summary.itemId}>
      <div><strong>{summary.playlist}</strong><span>多盘 · {summary.discCount} 张</span></div>
      <div><strong>{stateLabel(summary.state, summary.missingDiscCount)}</strong><span>已找到 {summary.presentDiscCount} / {summary.discCount} 张</span></div>
      {summary.ignoredFileCount ? <details><summary>已忽略 {summary.ignoredFileCount} 个未引用文件</summary><ul>{summary.ignoredFiles.map((name) => <li key={name}>{name}</li>)}</ul>{summary.ignoredFileCount > summary.ignoredFiles.length ? <p>只显示前 {summary.ignoredFiles.length} 个文件名。</p> : null}</details> : <span className="import-task-no-ignored">没有未引用文件</span>}
    </article>)}
  </div>;
}

function RejectedFiles({ detail }: { detail: DetailState | undefined }) {
  if (!detail || detail.status === "loading") {return <p>正在读取文件明细…</p>;}
  if (detail.status === "error") {return <p>文件明细读取失败，请稍后重试。</p>;}
  return <>{detail.value?.fileOutcomes.filter((file) => file.disposition === "REJECTED" && !file.resolution).map((file) => {
    const code = file.reasonCode ?? "REJECTED";
    return <div key={file.name}><strong title={file.name}>{file.name}</strong><span>{rejectionLabels[code] ?? "文件未通过导入规则"}</span><code tabIndex={0} title={rejectionDetails[code] ?? "该稳定错误码暂无补充说明"}>{code}</code></div>;
  })}</>;
}

function AlreadyImportedFiles({ detail }: { detail: DetailState | undefined }) {
  if (!detail || detail.status === "loading") {return <p>正在读取已导入文件…</p>;}
  if (detail.status === "error") {return <p>文件明细读取失败，请稍后重试。</p>;}
  return <><p>以下文件已关联到未删除的游戏，本次默认跳过，没有创建重复游戏。</p>{detail.value?.fileOutcomes.filter((file) => file.disposition === "ALREADY_IMPORTED").map((file) => <div key={file.uploadFileId}><strong title={file.name}>{file.name}</strong><span>游戏文件已经导入</span><code>ALREADY_IMPORTED</code></div>)}{detail.value?.alreadyImportedMatches?.map((match) => <div key={`${match.importItemId}:${match.existingGame.id}`}><strong>{match.existingGame.title}</strong><span>{match.existingGame.platformInstanceName}</span><Link href={`/games/${match.existingGame.id}`}>查看已有游戏</Link></div>)}</>;
}

function TaskStages({ attention, issueCount, stageIndex }: { attention: boolean; issueCount: number; stageIndex: number }) {
  return <div className="import-task-stages">{stages.map((stage, index) => {
    let className = "";
    let label = "等待";
    if (index < stageIndex) {className = "is-done"; label = "✓ 完成";}
    else if (index === stageIndex) {className = attention ? "has-error" : "is-current"; label = attention ? `${issueCount} 异常` : "处理中";}
    return <div className={className} key={stage}><small>{stage}</small><strong>{label}</strong></div>;
  })}</div>;
}

function TaskProblem({ isMultiDisc, item }: { isMultiDisc: boolean; item: ImportListItem }) {
  const summary = importTaskIssueSummary(item);
  if (item.state === "FAILED") {
    return <div className="import-task-problem"><p>{summary}。请根据错误码检查项目结构或归档后重新上传。</p><Link className="button secondary" href="/admin/imports/new">新建导入</Link></div>;
  }
  const description = isMultiDisc
    ? `${summary}。多盘目录必须重新选择完整 DIRECTORY，不能只复用原任务中被拒绝的文件。`
    : `${summary}。可以复用服务器已经保存的文件，重新选择平台目录并再次识别，无需重新上传。`;
  return <div className="import-task-problem"><p>{description}</p><Link className="button secondary" href={isMultiDisc ? "/admin/imports/new" : `/admin/imports/new?fromImportJobId=${item.id}`}>{isMultiDisc ? "重新选择完整目录" : "重新配置并导入"}</Link></div>;
}

function TaskDetail({ attention, detail, isMultiDisc, issueCount, item, stageIndex }: {
  attention: boolean;
  detail: DetailState | undefined;
  isMultiDisc: boolean;
  issueCount: number;
  item: ImportListItem;
  stageIndex: number;
}) {
  const rejectedCount = item.unresolvedRejectedFileCount ?? item.rejectedFileCount;
  return <section className="import-task-detail" aria-label={`${item.platformInstanceName} 阶段详情`}>
    <TaskStages attention={attention} issueCount={issueCount} stageIndex={stageIndex} />
    {detail?.value?.payloadState === "RELEASED" ? <p className="import-task-detail-message">源文件已清理；这里只保留文件名、大小与处理结果。</p> : null}
    {detail?.value?.payloadState === "RELEASING" ? <p className="import-task-detail-message">正在清理源文件…</p> : null}
    {detail?.value?.payloadState === "FAILED" ? <p className="import-task-detail-message bad">源文件清理失败，可在任务中心重试清理任务。</p> : null}
    {isMultiDisc ? <MultiDiscTaskDetail detail={detail} /> : null}
    {issueCount ? <><TaskProblem isMultiDisc={isMultiDisc} item={item} />{rejectedCount ? <div className="import-task-rejections" aria-label="未被接受的文件"><RejectedFiles detail={detail} /></div> : null}</> : null}
    {item.alreadyImportedItemCount ? <div className="import-task-rejections" aria-label="已导入并跳过的文件"><AlreadyImportedFiles detail={detail} /></div> : null}
  </section>;
}

function TaskNextStep({ attention, item }: { attention: boolean; item: ImportListItem }) {
  let label = "后台会继续推进当前阶段";
  if (attention) {label = importTaskIssueSummary(item);}
  else if (item.state === "REVIEW_PENDING") {label = `${item.reviewPendingItemCount} 个条目等待确认`;}
  else if (item.state === "COMPLETED" && item.alreadyImportedItemCount) {label = `${item.alreadyImportedItemCount} 个条目因游戏文件已导入而跳过`;}
  else if (item.state === "COMPLETED") {label = "已完成本次入库";}
  const heading = attention ? "当前问题" : item.state === "COMPLETED" ? "结果" : "下一步";
  return <div className="import-task-next"><strong>{heading}</strong><small>{label}</small></div>;
}

function TaskActions({ expanded, isMultiDisc, issueCount, item, onToggle }: { expanded: boolean; isMultiDisc: boolean; issueCount: number; item: ImportListItem; onToggle: () => void }) {
  if (item.reviewPendingItemCount) {return <div className="import-task-actions"><Link aria-label="查看待审核" className="button" href={`/admin/reviews?importJobId=${item.id}`}>审核 {item.reviewPendingItemCount} 个条目</Link></div>;}
  if (issueCount) {return <div className="import-task-actions" />;}
  if (item.state === "COMPLETED" && item.alreadyImportedItemCount) {return <div className="import-task-actions"><button className="button secondary" type="button" aria-expanded={expanded} onClick={onToggle}>{expanded ? "收起详情" : "查看已跳过"}</button></div>;}
  if (item.state === "COMPLETED") {return <div className="import-task-actions"><Link className="button secondary" href="/admin/reviews/history">查看结果</Link></div>;}
  if (!isMultiDisc) {return <div className="import-task-actions"><button className="button secondary" type="button" aria-expanded={expanded} onClick={onToggle}>{expanded ? "收起详情" : "查看进度"}</button></div>;}
  return <div className="import-task-actions" />;
}

function ImportTaskEntry({ detail, expanded, item, onToggle }: { detail: DetailState | undefined; expanded: boolean; item: ImportListItem; onToggle: () => void }) {
  const progress = importTaskProgress(item);
  const stageIndex = importStageIndex(item);
  const attention = item.state === "PARTIAL_FAILURE" || item.state === "FAILED";
  const issueCount = importTaskIssueCount(item);
  const isMultiDisc = item.contentMode === "MULTI_DISC_M3U_V1";
  const importedNote = item.alreadyImportedItemCount ? ` · 已跳过 ${item.alreadyImportedItemCount} 个已导入条目` : "";
  return <div className="import-task-entry">
    <article className={`import-task-card${attention ? " has-error" : ""}`}>
      <div className="import-task-main"><h3>{formatTime(item.createdAtMs)} · {item.platformInstanceName}</h3><p>{item.totalItemCount} 个条目{isMultiDisc ? <> · <button className="import-task-inline-detail" type="button" aria-label="查看多盘目录" aria-expanded={expanded} onClick={onToggle}>多盘</button></> : null} · {importProviderLabels[item.metadataProvider] ?? item.metadataProvider} · 更新于 {formatTime(item.updatedAtMs)}{importedNote}</p></div>
      <StatusBadge tone={statusTone(item.state)}>{importStateLabels[item.state] ?? item.state}</StatusBadge>
      <div className="import-task-progress"><div><strong>{importTaskPhase(item)}</strong><span>{progress}%</span></div><div className="import-task-track"><i style={{ width: `${progress}%` }} /></div><div className="import-task-distribution"><span className="good">{item.reviewPendingItemCount} 待审核</span>{issueCount ? <button className="bad" type="button" aria-expanded={expanded} onClick={onToggle}>{issueCount} 异常</button> : <span className="neutral">0 异常</span>}</div></div>
      <TaskNextStep attention={attention} item={item} />
      <TaskActions expanded={expanded} isMultiDisc={isMultiDisc} issueCount={issueCount} item={item} onToggle={onToggle} />
    </article>
    {expanded ? <TaskDetail attention={attention} detail={detail} isMultiDisc={isMultiDisc} issueCount={issueCount} item={item} stageIndex={stageIndex} /> : null}
  </div>;
}

export function ImportTaskBoard({ initial, initialQuery = "", initialState = "" }: { initial: ListResponse<ImportListItem>; initialQuery?: string; initialState?: string }) {
  const [items, setItems] = useState(initial.items);
  const [nextCursor, setNextCursor] = useState(initial.nextCursor);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [filters, setFilters] = useState<ImportTaskFilters>({ query: initialQuery, directory: "", state: initialState });
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [details, setDetails] = useState<Record<string, DetailState>>({});
  const loadMoreRef = useRef<HTMLDivElement>(null);
  const visible = useMemo(() => filterImportTasks(items, filters), [items, filters]);
  const summary = useMemo(() => importTaskSummary(items), [items]);
  const directories = useMemo(() => [...new Set(items.map((item) => item.platformInstanceName))].sort((left, right) => left.localeCompare(right, "zh-CN")), [items]);

  function selectState(state: string) {
    setFilters((current) => ({ ...current, state }));
  }

  const loadMore = useCallback(async () => {
    if (!nextCursor || loadingMore) {return;}
    setLoadingMore(true);
    setLoadError("");
    try {
      const query = new URLSearchParams({ cursor: nextCursor, limit: "20" });
      const response = await fetch(`/api/v1/admin/imports?${query}`, { cache: "no-store" });
      if (!response.ok) {throw new Error("无法加载更多导入任务");}
      const page = await response.json() as ListResponse<ImportListItem>;
      setItems((current) => {
        const seen = new Set(current.map((item) => item.id));
        return [...current, ...page.items.filter((item) => !seen.has(item.id))];
      });
      setNextCursor(page.nextCursor);
    } catch (caught) {
      setLoadError(caught instanceof Error ? caught.message : "无法加载更多导入任务");
    } finally {
      setLoadingMore(false);
    }
  }, [loadingMore, nextCursor]);

  useEffect(() => {
    const target = loadMoreRef.current;
    if (!target || !nextCursor || typeof IntersectionObserver === "undefined") {return;}
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {void loadMore();}
    }, { rootMargin: "320px 0px" });
    observer.observe(target);
    return () => observer.disconnect();
  }, [loadMore, nextCursor]);

  const pollingKey = useMemo(() => items.filter((item) => activeImportStates.has(item.state)).map((item) => item.id).join(","), [items]);
  useEffect(() => {
    const pollingIds = pollingKey ? pollingKey.split(",") : [];
    if (!pollingIds.length) {return;}
    let disposed = false;
    let polling = false;
    const poll = async () => {
      if (polling || disposed) {return;}
      polling = true;
      try {
        const results = await Promise.all(pollingIds.map(async (id) => {
          const response = await fetch(`/api/v1/admin/imports/${id}`, { cache: "no-store", headers: { Accept: "application/json" } });
          return response.ok ? await response.json() as ImportDetail : null;
        }));
        if (!disposed) {
          const byId = new Map(results.filter((detail): detail is ImportDetail => detail !== null).map((detail) => [detail.importJobId, detail]));
          setItems((current) => current.map((item) => byId.has(item.id) ? refreshListItem(item, byId.get(item.id)!) : item));
        }
      } finally {
        polling = false;
      }
    };
    void poll();
    const timer = window.setInterval(() => void poll(), 1_000);
    return () => { disposed = true; window.clearInterval(timer); };
  }, [pollingKey]);

  async function toggleDetails(item: ImportListItem) {
    if (expandedId === item.id) {
      setExpandedId(null);
      return;
    }
    setExpandedId(item.id);
    const isMultiDisc = item.contentMode === "MULTI_DISC_M3U_V1";
    if ((!(item.unresolvedRejectedFileCount ?? item.rejectedFileCount) && !item.alreadyImportedItemCount && !isMultiDisc) || details[item.id]) {return;}
    setDetails((current) => ({ ...current, [item.id]: { status: "loading" } }));
    try {
      const response = await fetch(`/api/v1/admin/imports/${item.id}`, { headers: { Accept: "application/json" } });
      if (!response.ok) {throw new Error(`HTTP ${response.status}`);}
      const value = await response.json() as ImportDetail;
      setDetails((current) => ({ ...current, [item.id]: { status: "ready", value } }));
    } catch {
      setDetails((current) => ({ ...current, [item.id]: { status: "error" } }));
    }
  }

  return <div className="import-task-board">
    <section className="import-workflow-toolbar" aria-label="筛选导入任务">
      <label className="import-workflow-search"><span>搜索任务</span><span><AppIcon name="search" /><input type="search" value={filters.query} placeholder="输入目录名称或任务状态" onChange={(event) => setFilters((current) => ({ ...current, query: event.target.value }))} /></span></label>
      <label><span>目标目录</span><select value={filters.directory} onChange={(event) => setFilters((current) => ({ ...current, directory: event.target.value }))}><option value="">所有目录</option>{directories.map((directory) => <option key={directory}>{directory}</option>)}</select></label>
      <label><span>任务状态</span><select value={filters.state} onChange={(event) => selectState(event.target.value)}><option value="">所有状态</option><option value="RUNNING">运行中</option><option value="QUEUED">排队中</option><option value="ATTENTION">需要处理</option><option value="REVIEW_PENDING">等待审核</option><option value="COMPLETED">已完成</option></select></label>
    </section>
    <div className="import-workflow-chips" aria-label="任务摘要">
      <button className={filters.state === "" ? "is-active" : ""} type="button" onClick={() => selectState("")}>全部 {summary.total}</button>
      <button className={filters.state === "RUNNING" ? "is-active" : ""} type="button" onClick={() => selectState("RUNNING")}>运行中 {summary.running}</button>
      <button className={filters.state === "ATTENTION" ? "is-active" : ""} type="button" onClick={() => selectState("ATTENTION")}>需要处理 {summary.attention}</button>
      <button className={filters.state === "REVIEW_PENDING" ? "is-active" : ""} type="button" onClick={() => selectState("REVIEW_PENDING")}>等待审核 {summary.review}</button>
      <button className={filters.state === "COMPLETED" ? "is-active" : ""} type="button" onClick={() => selectState("COMPLETED")}>已完成 {summary.completed}</button>
    </div>
    {visible.length
      ? <div className="import-task-list">{visible.map((item) => <ImportTaskEntry detail={details[item.id]} expanded={expandedId === item.id} item={item} onToggle={() => void toggleDetails(item)} key={item.id} />)}</div>
      : <div className="import-workflow-empty"><h2>没有匹配的导入任务</h2><p>请调整搜索内容、目标目录或任务状态。</p></div>}
    <div ref={loadMoreRef} className="infinite-scroll-sentinel" aria-hidden="true" />
    <footer className="import-workflow-footer"><span>当前显示 {visible.length} / 已加载 {items.length} 个任务</span>{loadingMore ? <span role="status">正在加载下一页…</span> : nextCursor ? <button type="button" onClick={() => void loadMore()}>继续加载</button> : <span>已加载全部任务</span>}{loadError ? <button type="button" onClick={() => void loadMore()}>{loadError}，点击重试</button> : null}</footer>
  </div>;
}
