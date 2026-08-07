import { ButtonLink, PageHeader, StatusBadge } from "@/components/ui";
import { FlashToast } from "@/components/flash-toast";
import { ReviewActions, type ReviewWorkspace } from "@/features/reviews/review-actions";
import { type ReviewQueueItem } from "@/features/reviews/review-queue";
import { backendJSON, formatBytes, type ListResponse } from "@/lib/backend";
import Link from "next/link";

type DependencySnapshot = {
  machine?: string;
  datVersionId?: string;
  closure?: string[];
  dependencies?: Array<{ kind?: string; machine?: string; state?: string; requiredEntries?: string[] }>;
  missingEntries?: string[];
  mismatchedEntries?: string[];
  warnings?: string[];
};

type Review = ReviewWorkspace & {
  importJobId: string;
  platformInstance: { id: string; name: string };
  sourceManifest: { files: Array<{ logicalName: string; role: string; sizeBytes?: number; blobSha256?: string; sourceArchiveSha256?: string | null }> };
  validation: (NonNullable<ReviewWorkspace["validation"]> & { dependencySnapshot?: DependencySnapshot }) | null;
};

const validationLabels: Record<string, string> = { READY: "可以发布", BLOCKED: "缺少依赖", DEPENDENCY_MISSING: "缺少依赖", INCOMPATIBLE: "不兼容", NEEDS_VALIDATION: "等待检查", PENDING: "等待检查" };
const roleLabels: Record<string, string> = { CONTENT: "游戏文件", DOS_SOURCE: "DOS 游戏文件", COMPANION: "配套文件" };

function DependencySummary({ snapshot }: { snapshot: DependencySnapshot }) {
  if (!snapshot.machine) return <p className="muted-copy">当前平台无需额外的街机依赖检查。</p>;
  const issues = [
    ...(snapshot.missingEntries ?? []).map((entry) => `缺失：${entry}`),
    ...(snapshot.mismatchedEntries ?? []).map((entry) => `不匹配：${entry}`),
    ...(snapshot.warnings ?? []).map((entry) => `警告：${entry}`),
  ];
  return <section className="dependency-summary" aria-label="不可变依赖快照摘要">
    <div className="metric-line"><span>街机条目</span><strong>{snapshot.machine}</strong></div>
    <div className="metric-line"><span>游戏文件 / 外部依赖</span><strong>{snapshot.closure?.length ?? 0} / {snapshot.dependencies?.length ?? 0}</strong></div>
    {(snapshot.dependencies ?? []).map((dependency, index) => <div className="dependency-item" key={`${dependency.kind}-${dependency.machine}-${index}`}>
      <strong>{dependency.machine ?? "未知依赖"}</strong>
      <span>{dependency.state === "MISSING" ? "缺少文件" : dependency.state === "SATISFIED_EXTERNAL" ? "已满足" : "依赖检查"} · {dependency.requiredEntries?.length ?? 0} 个文件</span>
    </div>)}
    {issues.length ? <ul className="dependency-issues">{issues.map((issue) => <li key={issue}>{issue}</li>)}</ul> : <p className="status good">游戏文件和运行依赖均已就绪</p>}
    <details className="technical-details"><summary>技术详情</summary><code>{snapshot.datVersionId ?? "无数据目录版本"}</code></details>
  </section>;
}

function safeReturnTo(raw: string | undefined) {
  if (!raw) return "/admin/reviews";
  const parsed = new URL(raw, "http://retrom.invalid");
  const allowed = new Set(["q", "importJobId", "platformInstanceId", "blockerCode", "sort"]);
  if (parsed.origin !== "http://retrom.invalid" || parsed.pathname !== "/admin/reviews") return "/admin/reviews";
  for (const [name] of parsed.searchParams) if (!allowed.has(name) || parsed.searchParams.getAll(name).length !== 1) return "/admin/reviews";
  return `${parsed.pathname}${parsed.search}`;
}

export default async function ReviewDetailPage({ params, searchParams }: { params: Promise<{ itemId: string }>; searchParams: Promise<Record<string, string | string[] | undefined>> }) {
  const { itemId } = await params;
  const queryParameters = await searchParams;
  const returnTo = safeReturnTo(typeof queryParameters.returnTo === "string" ? queryParameters.returnTo : undefined);
  const listQuery = new URL(returnTo, "http://retrom.invalid").search;
  const [review, context] = await Promise.all([
    backendJSON<Review>(`/api/v1/admin/reviews/${itemId}`),
    backendJSON<ListResponse<ReviewQueueItem>>(`/api/v1/admin/reviews${listQuery}${listQuery ? "&" : "?"}limit=100`),
  ]);
  const validationStatus = review.validation?.status ?? "PENDING";
  const selectedIndex = context.items.findIndex((item) => item.itemId === itemId);
  const nextItem = context.items[selectedIndex + 1] ?? context.items[selectedIndex - 1] ?? null;
  return <><FlashToast />
    <PageHeader title="审核条目" description="核对游戏文件、兼容性和游戏信息，确认无误后发布。" actions={<ButtonLink href={returnTo} secondary>返回待审核列表</ButtonLink>} />
    <div className="review-detail-workbench">
      <aside className="panel review-context-queue"><div className="panel-head"><div><h2>当前队列</h2><p>{context.items.length} 条 · 可任意选择</p></div></div><nav aria-label="当前待审队列">{context.items.map((item) => <Link className={item.itemId === itemId ? "review-context-item is-active" : "review-context-item"} href={`/admin/reviews/${item.itemId}?returnTo=${encodeURIComponent(returnTo)}`} key={item.itemId}><strong>{item.draftTitle}</strong><small>{item.sourceDisplayName} · {validationLabels[item.validationStatus] ?? item.validationStatus}</small></Link>)}</nav></aside>
      <div className="stack">
        <section className="panel"><div className="panel-head"><div><h2>游戏文件</h2><p>确认文件和目标目录是否正确</p></div><StatusBadge tone="info">{review.sourceManifest.files.length} 个文件</StatusBadge></div><div className="panel-body stack"><p>目标目录：<strong>{review.platformInstance.name}</strong></p>{review.sourceManifest.files.map((file, index) => <div className="candidate" key={`${file.role}-${file.logicalName}-${index}`}><div className="metric-line"><span>{roleLabels[file.role] ?? "文件"}</span><strong>{file.logicalName}</strong></div>{file.sizeBytes === undefined ? null : <div className="metric-line"><span>大小</span><strong title={`${file.sizeBytes} bytes`}>{formatBytes(file.sizeBytes)}</strong></div>}<details className="technical-details"><summary>技术详情</summary><code>{file.blobSha256 ? `SHA-256 ${file.blobSha256}` : "无内容校验值"}{file.sourceArchiveSha256 ? `\n来源归档 ${file.sourceArchiveSha256}` : ""}</code></details></div>)}</div></section>
        <section className="panel"><div className="panel-head"><div><h2>兼容性检查</h2><p>确认游戏在目标目录中是否可以运行</p></div><StatusBadge tone={validationStatus === "READY" ? "good" : "warn"}>{validationLabels[validationStatus] ?? validationStatus}</StatusBadge></div><div className="panel-body stack">{review.validation ? <><div className="metric-line"><span>运行状态</span><strong>{validationLabels[review.validation.compatibilityCode] ?? validationLabels[validationStatus] ?? "需要检查"}</strong></div><details className="technical-details"><summary>检查记录</summary><code>{review.validation.id}</code></details>{review.validation.dependencySnapshot ? <DependencySummary snapshot={review.validation.dependencySnapshot} /> : null}</> : <p>兼容性检查尚未完成，暂时不能发布。</p>}</div></section>
      </div>
      <section className="panel"><div className="panel-head"><div><h2>元信息、候选与发布</h2><p>候选只在明确采用并保存草稿后成为来源</p></div><StatusBadge tone={review.selectedCandidateId || review.candidates.length ? "info" : "warn"}>{review.selectedCandidateId ? "已选候选" : review.candidates.length ? `${review.candidates.length} 个候选 · 未采用` : "人工元信息"}</StatusBadge></div><div className="panel-body"><ReviewActions review={review} returnTo={returnTo} nextItemId={nextItem?.itemId ?? null} /></div></section>
    </div>
  </>;
}
