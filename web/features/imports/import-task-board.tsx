"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { StatusBadge } from "@/components/ui";
import { formatTime } from "@/lib/backend";
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
};

export function ImportTaskBoard({ items, initialQuery = "", initialState = "" }: { items: ImportListItem[]; initialQuery?: string; initialState?: string }) {
  const [filters, setFilters] = useState<ImportTaskFilters>({ query: initialQuery, directory: "", state: initialState });
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [details, setDetails] = useState<Record<string, DetailState>>({});
  const visible = useMemo(() => filterImportTasks(items, filters), [items, filters]);
  const summary = useMemo(() => importTaskSummary(items), [items]);
  const directories = useMemo(() => [...new Set(items.map((item) => item.platformInstanceName))].sort((left, right) => left.localeCompare(right, "zh-CN")), [items]);

  function selectState(state: string) {
    setFilters((current) => ({ ...current, state }));
  }

  async function toggleDetails(item: ImportListItem) {
    if (expandedId === item.id) {
      setExpandedId(null);
      return;
    }
    setExpandedId(item.id);
    if ((!(item.unresolvedRejectedFileCount ?? item.rejectedFileCount) && !item.alreadyImportedItemCount) || details[item.id]) return;
    setDetails((current) => ({ ...current, [item.id]: { status: "loading" } }));
    try {
      const response = await fetch(`/api/v1/admin/imports/${item.id}`, { headers: { Accept: "application/json" } });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
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
    {visible.length ? <div className="import-task-list">{visible.map((item) => {
      const progress = importTaskProgress(item);
      const expanded = expandedId === item.id;
      const stageIndex = importStageIndex(item);
      const attention = item.state === "PARTIAL_FAILURE" || item.state === "FAILED";
      const issueCount = importTaskIssueCount(item);
      return <div className="import-task-entry" key={item.id}>
        <article className={`import-task-card${attention ? " has-error" : ""}`}>
          <div className="import-task-main"><h3>{formatTime(item.createdAtMs)} · {item.platformInstanceName}</h3><p>{item.totalItemCount} 个条目 · {importProviderLabels[item.metadataProvider] ?? item.metadataProvider} · 更新于 {formatTime(item.updatedAtMs)}{item.alreadyImportedItemCount ? ` · 已跳过 ${item.alreadyImportedItemCount} 个已导入条目` : ""}</p></div>
          <StatusBadge tone={statusTone(item.state)}>{importStateLabels[item.state] ?? item.state}</StatusBadge>
          <div className="import-task-progress"><div><strong>{importTaskPhase(item)}</strong><span>{progress}%</span></div><div className="import-task-track"><i style={{ width: `${progress}%` }} /></div><div className="import-task-distribution"><span className="good">{item.reviewPendingItemCount} 待审核</span><span className={issueCount ? "bad" : "neutral"}>{issueCount} 异常</span></div></div>
          <div className="import-task-next"><strong>{attention ? "当前问题" : item.state === "COMPLETED" ? "结果" : "下一步"}</strong><small>{attention ? importTaskIssueSummary(item) : item.state === "REVIEW_PENDING" ? `${item.reviewPendingItemCount} 个条目等待确认` : item.state === "COMPLETED" && item.alreadyImportedItemCount ? `${item.alreadyImportedItemCount} 个条目因游戏文件已导入而跳过` : item.state === "COMPLETED" ? "已完成本次入库" : "后台会继续推进当前阶段"}</small></div>
          <div className="import-task-actions">{item.reviewPendingItemCount ? <Link aria-label="查看待审核" className="button" href={`/admin/reviews?importJobId=${item.id}`}>审核 {item.reviewPendingItemCount} 个条目</Link> : item.state === "COMPLETED" && item.alreadyImportedItemCount ? <button className="button secondary" type="button" aria-expanded={expanded} onClick={() => void toggleDetails(item)}>{expanded ? "收起详情" : "查看已跳过"}</button> : item.state === "COMPLETED" ? <Link className="button secondary" href="/admin/reviews/history">查看结果</Link> : <button className="button secondary" type="button" aria-expanded={expanded} onClick={() => void toggleDetails(item)}>{expanded ? "收起详情" : attention ? "处理问题" : "查看进度"}</button>}</div>
        </article>
        {expanded ? <section className="import-task-detail" aria-label={`${item.platformInstanceName} 阶段详情`}><div className="import-task-stages">{stages.map((stage, index) => <div className={index < stageIndex ? "is-done" : index === stageIndex ? attention ? "has-error" : "is-current" : ""} key={stage}><small>{stage}</small><strong>{index < stageIndex ? "✓ 完成" : index === stageIndex ? attention ? `${issueCount} 异常` : "处理中" : "等待"}</strong></div>)}</div>{attention ? <><div className="import-task-problem"><p>{importTaskIssueSummary(item)}。可以复用服务器已经保存的文件，重新选择平台目录并再次识别，无需重新上传。</p><Link className="button secondary" href={`/admin/imports/new?fromImportJobId=${item.id}`}>重新配置并导入</Link></div>{(item.unresolvedRejectedFileCount ?? item.rejectedFileCount) ? <div className="import-task-rejections" aria-label="未被接受的文件">{details[item.id]?.status === "loading" || !details[item.id] ? <p>正在读取文件明细…</p> : details[item.id]?.status === "error" ? <p>文件明细读取失败，请稍后重试。</p> : details[item.id]?.value?.fileOutcomes.filter((file) => file.disposition === "REJECTED" && !file.resolution).map((file) => <div key={file.name}><strong title={file.name}>{file.name}</strong><span>{rejectionLabels[file.reasonCode ?? ""] ?? "文件未通过导入规则"}</span><code>{file.reasonCode ?? "REJECTED"}</code></div>)}</div> : null}</> : null}{item.alreadyImportedItemCount ? <div className="import-task-rejections" aria-label="已导入并跳过的文件">{details[item.id]?.status === "loading" || !details[item.id] ? <p>正在读取已导入文件…</p> : details[item.id]?.status === "error" ? <p>文件明细读取失败，请稍后重试。</p> : <><p>以下文件已关联到未删除的游戏，本次默认跳过，没有创建重复游戏。</p>{details[item.id]?.value?.fileOutcomes.filter((file) => file.disposition === "ALREADY_IMPORTED").map((file) => <div key={file.uploadFileId}><strong title={file.name}>{file.name}</strong><span>游戏文件已经导入</span><code>ALREADY_IMPORTED</code></div>)}{details[item.id]?.value?.alreadyImportedMatches?.map((match) => <div key={`${match.importItemId}:${match.existingGame.id}`}><strong>{match.existingGame.title}</strong><span>{match.existingGame.platformInstanceName}</span><Link href={`/games/${match.existingGame.id}`}>查看已有游戏</Link></div>)}</>}</div> : null}</section> : null}
      </div>;
    })}</div> : <div className="import-workflow-empty"><h2>没有匹配的导入任务</h2><p>请调整搜索内容、目标目录或任务状态。</p></div>}
    <footer className="import-workflow-footer">当前显示 {visible.length} / {items.length} 个任务</footer>
  </div>;
}
