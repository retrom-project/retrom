import { ButtonLink, Kpi, PageHeader, StatusBadge } from "@/components/ui";
import { importProviderLabels, importStateLabels, importTaskIssueCount, importTaskPhase, type ImportListItem } from "@/features/imports/import-workflow";
import { backendJSON, formatTime, type ListResponse } from "@/lib/backend";
import { statusTone } from "@/lib/status";

type Summary = { running: number; reviewPending: number; completed: number; failed: number };

export const metadata = { title: "游戏入库" };

export default async function ImportOverviewPage() {
  const [summary, imports] = await Promise.all([
    backendJSON<Summary>("/api/v1/admin/imports/summary"),
    backendJSON<ListResponse<ImportListItem>>("/api/v1/admin/imports"),
  ]);
  const recent = imports.items.slice(0, 3);
  const totalItems = recent.reduce((total, item) => total + item.totalItemCount, 0);
  const failedItems = recent.reduce((total, item) => total + importTaskIssueCount(item), 0);
  return (
    <div className="import-workflow-page import-overview-page">
      <PageHeader eyebrow="内容管理" title="游戏入库" description="管理游戏从上传、识别、运行检查到人工审核发布的完整流程。优先处理会阻塞入库进度的事项。" actions={<><ButtonLink href="/admin/imports/tasks" secondary>任务进度</ButtonLink><ButtonLink href="/admin/imports/new">＋ 导入游戏</ButtonLink></>} />
      <section className="import-priority-grid">
        <article><div><StatusBadge tone="warn">需要处理</StatusBadge><h2>待审核条目</h2><p>运行检查已完成，等待管理员确认发布信息。</p></div><div><strong>{summary.reviewPending}</strong><ButtonLink href="/admin/reviews" secondary>进入待审核 →</ButtonLink></div></article>
        <article><div><StatusBadge tone="bad">阻塞进度</StatusBadge><h2>异常任务</h2><p>文件、依赖或运行检查存在问题，需要先处理。</p></div><div><strong>{summary.failed}</strong><ButtonLink href="/admin/imports/tasks?state=ATTENTION" secondary>查看异常任务 →</ButtonLink></div></article>
      </section>
      <section className="kpi-grid import-kpi-grid">
        <Kpi label="运行中任务" value={summary.running} note="正在上传、识别或检查" tone="purple" />
        <Kpi label="等待审核" value={summary.reviewPending} note="已进入人工审核队列" tone="amber" />
        <Kpi label="异常条目" value={summary.failed} note="需要重新整理或补齐依赖" tone="slate" />
        <Kpi label="历史完成批次" value={summary.completed} note="已完成的全部导入任务" tone="cyan" />
      </section>
      <section className="panel import-pipeline-panel">
        <div className="panel-head"><div><h2>当前入库流水线</h2><p>展示当前任务规模和需要处理的真实条目。</p></div><StatusBadge tone="info">实时快照</StatusBadge></div>
        <div className="import-pipeline">
          <div className="is-done"><i>1</i><strong>上传与校验</strong><b>{summary.running} 个任务</b><span>浏览器上传并校验完整性</span></div>
          <div className="is-done"><i>2</i><strong>识别游戏</strong><b>{totalItems} 个条目</b><span>按平台规则识别文件结构</span></div>
          <div className={failedItems ? "has-warning" : "is-done"}><i>3</i><strong>运行检查</strong><b>{failedItems ? `${failedItems} 个异常` : "当前无异常"}</b><span>核对核心、BIOS 与依赖</span></div>
          <div><i>4</i><strong>获取游戏信息</strong><b>{summary.running} 批处理中</b><span>使用任务创建时的信息源</span></div>
          <div><i>5</i><strong>人工审核</strong><b>{summary.reviewPending} 待审核</b><span>逐条确认最终发布内容</span></div>
          <div><i>6</i><strong>发布</strong><b>{summary.completed} 批已完成</b><span>进入用户游戏库</span></div>
        </div>
      </section>
      <section className="panel import-recent-panel"><div className="panel-head"><div><h2>最近任务</h2><p>这里只显示摘要；完整运行态统一进入任务进度。</p></div><ButtonLink href="/admin/imports/tasks" secondary>查看全部</ButtonLink></div>{recent.length ? <div className="import-recent-list">{recent.map((item) => { const issueCount = importTaskIssueCount(item); return <article key={item.id}><div><h3>{formatTime(item.createdAtMs)} · {item.platformInstanceName}</h3><p>{item.totalItemCount} 个条目 · {importProviderLabels[item.metadataProvider] ?? item.metadataProvider}</p></div><StatusBadge tone={statusTone(item.state)}>{importStateLabels[item.state] ?? item.state}</StatusBadge><span>{importTaskPhase(item)}</span><span>{item.reviewPendingItemCount ? `${item.reviewPendingItemCount} 个待审核` : issueCount ? `${issueCount} 个异常` : "后台处理中"}</span>{item.reviewPendingItemCount ? <ButtonLink href={`/admin/reviews?importJobId=${item.id}`} secondary>审核</ButtonLink> : <ButtonLink href="/admin/imports/tasks" secondary>查看</ButtonLink>}</article>; })}</div> : <div className="import-workflow-empty"><h2>还没有导入任务</h2><p>选择游戏文件或目录，创建第一批入库任务。</p></div>}</section>
    </div>
  );
}
