import { ButtonLink, Kpi, PageHeader } from "@/components/ui";
import { backendJSON } from "@/lib/backend";

type Summary = { running: number; reviewPending: number; completed: number; failed: number };

export const metadata = { title: "游戏入库" };

export default async function ImportOverviewPage() {
  const summary = await backendJSON<Summary>("/api/v1/admin/imports/summary");
  return (
    <>
      <PageHeader eyebrow="内容管理" title="游戏入库" description="掌握导入进度，优先处理需要确认或重试的内容。" actions={<><ButtonLink href="/admin/imports/tasks" secondary>任务进度</ButtonLink><ButtonLink href="/admin/imports/new">＋ 新建导入</ButtonLink></>} />
      <section className="kpi-grid">
        <Kpi label="运行中任务" value={summary.running} note="包含排队、处理与有界取消" tone="purple" />
        <Kpi label="待审核条目" value={summary.reviewPending} note="逐条确认候选与兼容性" tone="amber" />
        <Kpi label="异常任务" value={summary.failed} note="查看原因并决定是否重试" tone="slate" />
        <Kpi label="已完成任务" value={summary.completed} note="全部批次的完成数量" tone="cyan" />
      </section>
      <section className="panel">
        <div className="panel-head"><div><h2>入库流程</h2><p>内容会依次经过以下步骤</p></div></div>
        <div className="pipeline">
          {[['1','上传内容','校验文件并避免重复'],['2','识别游戏','分析文件与目录结构'],['3','检查兼容性','确认运行核心和必要依赖'],['4','准备游戏信息','查找标题、封面和简介'],['5','人工审核', summary.reviewPending ? `${summary.reviewPending} 项待处理` : '逐条确认后发布']].map(([number,label,description]) => <div className="pipeline-step" key={number}><i>{number}</i><strong>{label}</strong><span>{description}</span></div>)}
        </div>
      </section>
      <div className="split-grid" style={{ marginTop: 20 }}>
        <section className="panel"><div className="panel-head"><div><h2>需要处理</h2><p>从最影响入库进度的事项开始</p></div></div><div className="panel-body action-list"><ButtonLink href="/admin/reviews" secondary><span>待审核条目</span><strong>{summary.reviewPending}</strong></ButtonLink><ButtonLink href="/admin/imports/tasks?state=FAILED" secondary><span>异常任务</span><strong>{summary.failed}</strong></ButtonLink></div></section>
        <section className="panel"><div className="panel-head"><div><h2>快速开始</h2><p>从这台电脑选择游戏文件或目录</p></div></div><div className="panel-body"><p>系统会在上传完成后自动校验、识别并检查游戏是否可以运行。</p><ButtonLink href="/admin/imports/new">选择内容</ButtonLink></div></section>
      </div>
    </>
  );
}
