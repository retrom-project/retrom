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
  importTaskPhase,
  importTaskProgress,
  importTaskSummary,
  type ImportListItem,
  type ImportTaskFilters,
} from "./import-workflow";

const stages = ["上传", "识别", "运行检查", "游戏信息", "人工审核", "发布"];

export function ImportTaskBoard({ items, initialQuery = "", initialState = "" }: { items: ImportListItem[]; initialQuery?: string; initialState?: string }) {
  const [filters, setFilters] = useState<ImportTaskFilters>({ query: initialQuery, directory: "", state: initialState });
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const visible = useMemo(() => filterImportTasks(items, filters), [items, filters]);
  const summary = useMemo(() => importTaskSummary(items), [items]);
  const directories = useMemo(() => [...new Set(items.map((item) => item.platformInstanceName))].sort((left, right) => left.localeCompare(right, "zh-CN")), [items]);

  function selectState(state: string) {
    setFilters((current) => ({ ...current, state }));
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
      return <div className="import-task-entry" key={item.id}>
        <article className={`import-task-card${attention ? " has-error" : ""}`}>
          <div className="import-task-main"><h3>{formatTime(item.createdAtMs)} · {item.platformInstanceName}</h3><p>{item.totalItemCount} 个条目 · {importProviderLabels[item.metadataProvider] ?? item.metadataProvider} · 更新于 {formatTime(item.updatedAtMs)}</p></div>
          <StatusBadge tone={statusTone(item.state)}>{importStateLabels[item.state] ?? item.state}</StatusBadge>
          <div className="import-task-progress"><div><strong>{importTaskPhase(item)}</strong><span>{progress}%</span></div><div className="import-task-track"><i style={{ width: `${progress}%` }} /></div><div className="import-task-distribution"><span className="good">{item.reviewPendingItemCount} 待审核</span><span className={item.failedItemCount ? "bad" : "neutral"}>{item.failedItemCount} 异常</span></div></div>
          <div className="import-task-next"><strong>{attention ? "当前问题" : item.state === "COMPLETED" ? "结果" : "下一步"}</strong><small>{attention ? `${item.failedItemCount} 个条目需要处理` : item.state === "REVIEW_PENDING" ? `${item.reviewPendingItemCount} 个条目等待确认` : item.state === "COMPLETED" ? "已完成本次入库" : "后台会继续推进当前阶段"}</small></div>
          <div className="import-task-actions">{item.reviewPendingItemCount ? <Link aria-label="查看待审核" className="button" href={`/admin/reviews?importJobId=${item.id}`}>审核 {item.reviewPendingItemCount} 个条目</Link> : item.state === "COMPLETED" ? <Link className="button secondary" href="/admin/reviews/history">查看结果</Link> : <button className="button secondary" type="button" aria-expanded={expanded} onClick={() => setExpandedId(expanded ? null : item.id)}>{expanded ? "收起详情" : attention ? "处理问题" : "查看进度"}</button>}</div>
        </article>
        {expanded ? <section className="import-task-detail" aria-label={`${item.platformInstanceName} 阶段详情`}><div className="import-task-stages">{stages.map((stage, index) => <div className={index < stageIndex ? "is-done" : index === stageIndex ? attention ? "has-error" : "is-current" : ""} key={stage}><small>{stage}</small><strong>{index < stageIndex ? "✓ 完成" : index === stageIndex ? attention ? `${item.failedItemCount} 异常` : "处理中" : "等待"}</strong></div>)}</div>{attention ? <p className="import-task-problem">当前有 {item.failedItemCount} 个条目未完成。重新整理不支持的内容或补齐运行依赖后，再创建新的导入任务。</p> : null}</section> : null}
      </div>;
    })}</div> : <div className="import-workflow-empty"><h2>没有匹配的导入任务</h2><p>请调整搜索内容、目标目录或任务状态。</p></div>}
    <footer className="import-workflow-footer">当前显示 {visible.length} / {items.length} 个任务</footer>
  </div>;
}
