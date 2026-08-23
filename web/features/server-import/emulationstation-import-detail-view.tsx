"use client";

import Link from "next/link";
import type { ReactNode } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast } from "@/components/flash-toast";
import { TagChips } from "@/components/tag-picker";
import { StatusBadge } from "@/components/ui";
import {
  emulationStationOutcomeLabels,
  emulationStationPhaseLabels,
  emulationStationStateLabels,
  emulationStationStateTone,
  type EmulationStationCollection,
  type EmulationStationGamelist,
  type EmulationStationImportSummary,
  type EmulationStationItem,
} from "./emulationstation-import-model";

export type EmulationStationDetailFilters = {
  query: string;
  outcome: string;
  warning: string;
  collectionId: string;
};

export type EmulationStationDetailViewProps = {
  summary: EmulationStationImportSummary;
  items: EmulationStationItem[];
  nextCursor: string | null;
  draft: EmulationStationDetailFilters;
  collections: EmulationStationCollection[];
  gamelists: EmulationStationGamelist[];
  busy: boolean;
  error: string;
  cancelOpen: boolean;
  deleteOpen: boolean;
  mappingOpen: boolean;
  mappingDrawer: ReactNode;
  onDraft: (draft: EmulationStationDetailFilters) => void;
  onApplyFilters: () => void;
  onCancelOpen: (open: boolean) => void;
  onDeleteOpen: (open: boolean) => void;
  onCancel: () => void;
  onDelete: () => void;
  onRetry: () => void;
  onMappingOpen: (open: boolean) => void;
  onLoadMore: () => void;
  onDismissError: () => void;
};

function outcomeTone(item: EmulationStationItem): "good" | "warn" | "bad" | "info" {
  if (item.executionState === "PUBLISHED") {
    return "good";
  }
  if (item.executionState === "REVIEW_PENDING") {
    return "info";
  }
  const failed = item.executionState.startsWith("BLOCKED")
    || ["SOURCE_CHANGED", "READ_FAILED", "COMMIT_FAILED"].includes(item.executionState);
  return failed ? "bad" : "warn";
}

function mediaTone(state: string): "good" | "warn" | "info" {
  if (state === "READY") {
    return "good";
  }
  return state === "WARNING" ? "warn" : "info";
}

function SourceFlags({ item }: { item: EmulationStationItem }) {
  const labels = [
    item.sourceFlags.hidden ? "hidden" : "",
    item.sourceFlags.adult ? "adult" : "",
    item.sourceFlags.kidGame ? "kidgame" : "",
  ].filter(Boolean);
  if (!labels.length) {
    return null;
  }
  return <span className="emulationstation-source-flags" aria-label={`来源标记：${labels.join("、")}`}>
    {labels.map((label) => <small key={label}>{label}</small>)}
  </span>;
}

function FailureDetails({ item }: { item: EmulationStationItem }) {
  if (!item.failureDetails) {
    return null;
  }
  return <dl>
    <div><dt>失败阶段</dt><dd><code>{item.failureDetails.stage}</code></dd></div>
    <div><dt>内部操作</dt><dd><code>{item.failureDetails.operation}</code></dd></div>
    <div><dt>原因分类</dt><dd><code>{item.failureDetails.causeCode}</code></dd></div>
    {item.failureDetails.relativePath ? <div>
      <dt>相对路径</dt><dd><code>{item.failureDetails.relativePath}</code></dd>
    </div> : null}
  </dl>;
}

function RuntimeCheck({ item }: { item: EmulationStationItem }) {
  if (!item.runtimeCheck) {
    return null;
  }
  return <dl>
    <div><dt>运行检查</dt><dd>{item.runtimeCheck.status}</dd></div>
    {item.runtimeCheck.coreName ? <div><dt>检查核心</dt><dd>{item.runtimeCheck.coreName}</dd></div> : null}
    {item.runtimeCheck.machine ? <div><dt>Machine</dt><dd><code>{item.runtimeCheck.machine}</code></dd></div> : null}
    {item.runtimeCheck.missingEntries.length ? <div>
      <dt>缺失条目</dt>
      <dd>{item.runtimeCheck.missingEntries.map((entry) => <code key={entry}>{entry}</code>)}</dd>
    </div> : null}
  </dl>;
}

function SourceWarnings({ item }: { item: EmulationStationItem }) {
  if (!item.warnings.length) {
    return null;
  }
  return <div className="emulationstation-warning-list">
    <strong>来源提示</strong>
    {item.warnings.map((warning, index) => <code key={`${warning.code}-${index}`}>
      {warning.code}{warning.field ? ` · ${warning.field}` : ""}
    </code>)}
  </div>;
}

function ItemDiagnostics({ item }: { item: EmulationStationItem }) {
  const code = item.runtimeCheck?.code ?? item.errorCode ?? item.discoveryCode;
  if (!code && !item.failureDetails && !item.warnings.length) {
    return null;
  }
  return <details className="pegasus-runtime-diagnostic">
    <summary>查看来源与运行诊断</summary>
    <div className="pegasus-runtime-diagnostic-body">
      <header><div>
        <strong>{code ?? "来源包含提示"}</strong>
        <p>诊断只展示稳定 code、相对清单位置和有界结构，不显示宿主路径或 XML 原文。</p>
      </div></header>
      <FailureDetails item={item} />
      <RuntimeCheck item={item} />
      <SourceWarnings item={item} />
    </div>
  </details>;
}

function ItemAction({ item, reviewURL }: { item: EmulationStationItem; reviewURL: string }) {
  if (item.reviewItemId && item.executionState === "REVIEW_PENDING") {
    const href = `/admin/reviews/${item.reviewItemId}?returnTo=${encodeURIComponent(reviewURL)}`;
    return <Link className="button compact" href={href}>
      {item.runtimeCheck?.status === "READY" ? "审核并决定" : "处理运行问题"}
    </Link>;
  }
  if (item.publishedGameId) {
    return <Link href={`/games/${item.publishedGameId}`}>查看游戏</Link>;
  }
  if (item.existingGameId) {
    return <Link href={`/games/${item.existingGameId}`}>已有游戏</Link>;
  }
  if (item.executionState === "REVIEW_DISCARDED") {
    return <small>管理员已丢弃</small>;
  }
  return <span>—</span>;
}

function ItemMedia({ item }: { item: EmulationStationItem }) {
  if (item.payloadState === "RELEASED") {
    return <StatusBadge tone="good">源文件已清理</StatusBadge>;
  }
  return <>
    <StatusBadge tone={mediaTone(item.media.cover)}>封面 {item.media.cover}</StatusBadge>
    <StatusBadge tone={mediaTone(item.media.video)}>视频 {item.media.video}</StatusBadge>
  </>;
}

function ResultRow({ item, reviewURL }: { item: EmulationStationItem; reviewURL: string }) {
  const result = item.errorCode
    ?? item.discoveryCode
    ?? item.warnings[0]?.code
    ?? (item.executionState === "REVIEW_PENDING" ? "等待管理员审核" : "无附加结果码");
  return <article role="row">
    <div role="cell">
      <h3>{item.title}</h3>
      <SourceFlags item={item} />
      <TagChips tags={item.tags} limit={2} ariaLabel={`${item.title} 的标签`} />
      <p>{item.collectionName ?? "无有效 Collection"} → {item.targetPlatformInstanceName ?? "未映射"}</p>
      <small>{item.gamelistRelativePath} · {item.contentKind ?? "内容类型待定"}</small>
    </div>
    <div role="cell" className="pegasus-result-media"><ItemMedia item={item} /></div>
    <div role="cell">
      <StatusBadge tone={outcomeTone(item)}>{emulationStationOutcomeLabels[item.executionState]}</StatusBadge>
      <small>{result}</small>
    </div>
    <div role="cell"><ItemAction item={item} reviewURL={reviewURL} /></div>
    <div role="cell" className="pegasus-runtime-diagnostic-cell"><ItemDiagnostics item={item} /></div>
  </article>;
}

function DetailHeaderActions({ props, reviewURL }: { props: EmulationStationDetailViewProps; reviewURL: string }) {
  const canCancel = ["SCANNING", "QUEUED", "RUNNING"].includes(props.summary.state);
  const deletable = props.summary.state === "AWAITING_MAPPING" || props.summary.state === "EXPIRED";
  return <div>
    {canCancel ? <button type="button" className="button secondary" disabled={props.busy} onClick={() => props.onCancelOpen(true)}>
      取消任务
    </button> : null}
    {props.summary.retryable ? <button type="button" className="button secondary" disabled={props.busy} onClick={props.onRetry}>
      重试失败条目
    </button> : null}
    {props.summary.counts.reviewPending ? <Link href={reviewURL} className="button">
      逐项审核 {props.summary.counts.reviewPending} 个游戏
    </Link> : null}
    {props.summary.state === "AWAITING_MAPPING" ? <button
      type="button"
      className="button"
      disabled={props.busy}
      onClick={() => props.onMappingOpen(true)}
    >继续映射</button> : <Link href="/admin/imports/server?action=emulationstation" className="button secondary">
      新建 EmulationStation 导入
    </Link>}
    {deletable ? <button type="button" className="button danger" disabled={props.busy} onClick={() => props.onDeleteOpen(true)}>
      删除计划
    </button> : null}
  </div>;
}

function DetailHeader({ props, reviewURL, phase }: {
  props: EmulationStationDetailViewProps;
  reviewURL: string;
  phase: string;
}) {
  return <section className="server-import-detail-head panel">
    <div>
      <StatusBadge tone={emulationStationStateTone(props.summary.state)}>{emulationStationStateLabels[props.summary.state]}</StatusBadge>
      <h2>{props.summary.root.label} / {props.summary.sourceRelativePath || "根目录"}</h2>
      <p aria-live="polite">{phase}</p>
    </div>
    <DetailHeaderActions props={props} reviewURL={reviewURL} />
  </section>;
}

function DetailSummary({ summary }: { summary: EmulationStationImportSummary }) {
  return <section className="runtime-kpis" aria-label="EmulationStation 导入摘要">
    <article>
      <small>扫描范围</small>
      <strong>{summary.counts.games}</strong>
      <p>{summary.counts.gamelists} 份清单 · {summary.counts.invalidGamelists} 份无效</p>
    </article>
    <article className={summary.counts.reviewPending ? "has-warning" : ""}>
      <small>等待逐项审核</small>
      <strong>{summary.counts.reviewPending}</strong>
      <p>不会自动发布到游戏库</p>
    </article>
    <article className="has-success">
      <small>已发布 / 已丢弃 / 已存在</small>
      <strong>{summary.counts.published} / {summary.counts.reviewDiscarded} / {summary.counts.existing}</strong>
      <p>均保留来源与审核证据</p>
    </article>
    <article className={summary.counts.blocked + summary.counts.failed ? "has-danger" : ""}>
      <small>阻断 / 失败</small>
      <strong>{summary.counts.blocked} / {summary.counts.failed}</strong>
      <p>{summary.counts.mediaWarnings} 个媒体警告</p>
    </article>
  </section>;
}

function GamelistEvidence({ gamelists }: { gamelists: EmulationStationGamelist[] }) {
  return <details className="panel emulationstation-gamelist-evidence">
    <summary>查看 {gamelists.length} 份 Gamelist 扫描结果</summary>
    <div>{gamelists.map((item) => <p key={item.relativePath}>
      <code>{item.relativePath}</code>
      <StatusBadge tone={item.parseState === "VALID" ? "good" : "bad"}>
        {item.parseState === "VALID" ? `${item.gameCount} 个游戏` : item.errorCode ?? "解析失败"}
      </StatusBadge>
      <small>folder {item.folderCount}{item.providerPresent ? " · 检测到 provider" : ""}</small>
    </p>)}</div>
  </details>;
}

function ResultFilters({ props }: { props: EmulationStationDetailViewProps }) {
  return <form
    className="server-import-result-filters panel pegasus-result-filters"
    onSubmit={(event) => {
      event.preventDefault();
      props.onApplyFilters();
    }}
  >
    <label>
      <span>搜索标题</span>
      <input type="search" value={props.draft.query} onChange={(event) => props.onDraft({ ...props.draft, query: event.target.value })} />
    </label>
    <label>
      <span>结果</span>
      <select value={props.draft.outcome} onChange={(event) => props.onDraft({ ...props.draft, outcome: event.target.value })}>
        <option value="">全部结果</option>
        {Object.entries(emulationStationOutcomeLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select>
    </label>
    <label>
      <span>媒体或来源提示</span>
      <input
        value={props.draft.warning}
        placeholder="例如 EMULATIONSTATION_MEDIA_MISSING"
        onChange={(event) => props.onDraft({ ...props.draft, warning: event.target.value })}
      />
    </label>
    <label>
      <span>Collection</span>
      <select value={props.draft.collectionId} onChange={(event) => props.onDraft({ ...props.draft, collectionId: event.target.value })}>
        <option value="">全部 Collection</option>
        {props.collections.map((collection) => <option value={collection.id} key={collection.id}>{collection.displayName}</option>)}
      </select>
    </label>
    <button type="submit" className="button secondary compact" disabled={props.busy}>
      {props.busy ? "正在筛选…" : "应用筛选"}
    </button>
  </form>;
}

function DetailResults({ props, reviewURL }: { props: EmulationStationDetailViewProps; reviewURL: string }) {
  return <section className="server-import-results">
    <div className="runtime-section-heading">
      <div><h2>准备与审核结果</h2><p>来源 flag 是核对提示，不代表权限或兼容性结论。</p></div>
      <span>{props.items.length} / {props.summary.counts.games} 项</span>
    </div>
    <ResultFilters props={props} />
    <div className="pegasus-result-table emulationstation-result-table" role="table" aria-label="EmulationStation 导入结果">
      {props.items.map((item) => <ResultRow key={item.id} item={item} reviewURL={reviewURL} />)}
    </div>
    {props.nextCursor ? <button
      type="button"
      className="button secondary server-import-history-more"
      disabled={props.busy}
      onClick={props.onLoadMore}
    >加载更多结果</button> : null}
  </section>;
}

function ReviewCallout({ count, reviewURL }: { count: number; reviewURL: string }) {
  if (!count) {
    return null;
  }
  return <section className="pegasus-review-callout panel">
    <div>
      <span>下一步 · 人工审核</span>
      <h2>内容已准备好，但尚未进入游戏库</h2>
      <p>逐条核对运行检查、标题、媒体和来源 flag；只有明确通过的游戏才会发布。</p>
    </div>
    <Link href={reviewURL} className="button">打开这批审核队列</Link>
  </section>;
}

function phaseLabel(summary: EmulationStationImportSummary) {
  if (summary.phase) {
    return emulationStationPhaseLabels[summary.phase];
  }
  const terminal = ["COMPLETED", "PARTIAL_FAILURE", "FAILED", "CANCELLED", "EXPIRED"].includes(summary.state);
  if (!terminal) {
    return "等待处理";
  }
  return summary.counts.reviewPending ? `已准备 ${summary.counts.reviewPending} 个待审核游戏` : "后台准备任务已结束";
}

export function EmulationStationImportDetailView(props: EmulationStationDetailViewProps) {
  const reviewURL = `/admin/reviews?emulationStationImportId=${encodeURIComponent(props.summary.id)}`;
  return <div className="server-import-detail-page emulationstation-detail-page">
    <DetailHeader props={props} reviewURL={reviewURL} phase={phaseLabel(props.summary)} />
    <ReviewCallout count={props.summary.counts.reviewPending} reviewURL={reviewURL} />
    <DetailSummary summary={props.summary} />
    <GamelistEvidence gamelists={props.gamelists} />
    {props.summary.lastErrorCode ? <p className="server-import-error panel">
      <strong>{props.summary.lastErrorCode}</strong>
      <span>外部 source 不属于备份；目录变化时请按提示重新扫描。</span>
    </p> : null}
    <DetailResults props={props} reviewURL={reviewURL} />
    <ConfirmDialog
      open={props.cancelOpen}
      title="取消这次 EmulationStation 任务？"
      description="已经生成的审核事项会保留，尚未处理的项目会在安全检查点停止。"
      confirmLabel="确认取消"
      tone="danger"
      busy={props.busy}
      onCancel={() => props.onCancelOpen(false)}
      onConfirm={props.onCancel}
    />
    <ConfirmDialog
      open={props.deleteOpen}
      title="删除这份未执行计划？"
      description="只删除等待映射或已过期且没有执行结果的计划。来源目录不会改变。"
      confirmLabel="删除计划"
      tone="danger"
      busy={props.busy}
      onCancel={() => props.onDeleteOpen(false)}
      onConfirm={props.onDelete}
    />
    {props.mappingOpen ? props.mappingDrawer : null}
    <Toast toast={props.error ? { message: props.error, tone: "bad" } : null} onDismiss={props.onDismissError} />
  </div>;
}
