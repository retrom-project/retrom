import { ButtonLink, Kpi, PageHeader, StatusBadge } from "@/components/ui";
import { recentImportActivities, type ImportOverviewSummary, type PegasusImportSummary } from "@/features/imports/import-overview";
import type { ImportListItem } from "@/features/imports/import-workflow";
import { formatTime, type ListResponse } from "@/lib/backend";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "游戏入库" };

export default async function ImportOverviewPage() {
  const [summary, imports, pegasusImports] = await Promise.all([
    backendJSON<ImportOverviewSummary>("/api/v1/admin/imports/summary"),
    backendJSON<ListResponse<ImportListItem>>("/api/v1/admin/imports?limit=3"),
    backendJSON<ListResponse<PegasusImportSummary>>("/api/v1/admin/pegasus-imports?limit=3"),
  ]);
  const recent = recentImportActivities(imports.items, pegasusImports.items);
  return (
    <div className="import-workflow-page import-overview-page">
      <PageHeader eyebrow="内容管理" title="游戏入库" description="管理游戏从上传、识别、运行检查到人工审核发布的完整流程。优先处理会阻塞入库进度的事项。" actions={<><ButtonLink href="/admin/imports/tasks" secondary>任务进度</ButtonLink><ButtonLink href="/admin/imports/new">＋ 导入游戏</ButtonLink></>} />
      <section className="import-priority-grid">
        <article><div><StatusBadge tone="warn">需要处理</StatusBadge><h2>待审核条目</h2><p>运行检查已完成，等待管理员确认发布信息。</p></div><div><strong>{summary.reviewPending}</strong><ButtonLink href="/admin/reviews" secondary>进入待审核</ButtonLink></div></article>
        <article><div><StatusBadge tone="bad">阻塞进度</StatusBadge><h2>异常任务</h2><p>文件、依赖或运行检查存在问题，需要先处理。</p></div><div><strong>{summary.failed}</strong>{summary.failed ? <span className="import-priority-actions">{summary.ordinaryFailed ? <ButtonLink href="/admin/imports/tasks?state=ATTENTION" secondary>普通任务</ButtonLink> : null}{summary.pegasusFailed ? <ButtonLink href="/admin/imports/server" secondary>本地扫描</ButtonLink> : null}</span> : <ButtonLink href="/admin/imports/tasks" secondary>查看任务</ButtonLink>}</div></article>
      </section>
      <section className="kpi-grid import-kpi-grid">
        <Kpi label="进行中批次" value={summary.running} note="正在扫描、配置、识别或检查" tone="purple" />
        <Kpi label="等待审核" value={summary.reviewPending} note="已进入人工审核队列" tone="amber" />
        <Kpi label="异常条目" value={summary.issueItems} note="需要重新整理或补齐依赖" tone="slate" />
        <Kpi label="历史完成批次" value={summary.completed} note="按用户发起的顶层导入统计" tone="cyan" />
      </section>
      <section className="panel import-pipeline-panel">
        <div className="panel-head"><div><h2>当前入库流水线</h2><p>展示当前任务规模和需要处理的真实条目。</p></div><StatusBadge tone="info">实时快照</StatusBadge></div>
        <div className="import-pipeline">
          <div className="is-done"><i>1</i><strong>上传与校验</strong><b>{summary.running} 个批次</b><span>浏览器上传或服务器扫描</span></div>
          <div className="is-done"><i>2</i><strong>识别游戏</strong><b>{summary.processingItems} 个处理中</b><span>当前非终态批次的已知条目</span></div>
          <div className={summary.issueItems ? "has-warning" : "is-done"}><i>3</i><strong>运行检查</strong><b>{summary.issueItems ? `${summary.issueItems} 个异常` : "当前无异常"}</b><span>核对核心、BIOS 与依赖</span></div>
          <div><i>4</i><strong>获取游戏信息</strong><b>{summary.running} 批处理中</b><span>使用任务创建时的信息源</span></div>
          <div><i>5</i><strong>人工审核</strong><b>{summary.reviewPending} 待审核</b><span>逐条确认最终发布内容</span></div>
          <div><i>6</i><strong>发布</strong><b>{summary.publishedItems} 个已发布</b><span>实际进入用户游戏库</span></div>
        </div>
      </section>
      <section className="panel import-recent-panel"><div className="panel-head"><div><h2>最近任务</h2><p>按用户发起的批次展示；Pegasus 目录导入只占一行。</p></div><div className="import-recent-actions"><ButtonLink href="/admin/imports/tasks" secondary>普通任务</ButtonLink><ButtonLink href="/admin/imports/server" secondary>本地扫描</ButtonLink></div></div>{recent.length ? <div className="import-recent-list">{recent.map((item) => <article key={`${item.kind}-${item.id}`}><div><h3>{formatTime(item.createdAtMs)} · {item.title}</h3><p>{item.totalItemCount} 个条目 · {item.sourceLabel}</p></div><StatusBadge tone={item.tone}>{item.stateLabel}</StatusBadge><span>{item.phase}</span><span>{item.outcome}</span><ButtonLink href={item.actionHref} secondary>{item.actionLabel}</ButtonLink></article>)}</div> : <div className="import-workflow-empty"><h2>还没有导入任务</h2><p>选择游戏文件或目录，创建第一批入库任务。</p></div>}</section>
    </div>
  );
}
