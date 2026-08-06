import { ButtonLink, Kpi, PageHeader, StatusBadge } from "@/components/ui";
import { backendJSON } from "@/lib/backend";

type Summary = { running: number; reviewPending: number; completed: number; failed: number };

export const metadata = { title: "游戏入库" };

export default async function ImportOverviewPage() {
  const summary = await backendJSON<Summary>("/api/v1/admin/imports/summary");
  return (
    <>
      <PageHeader eyebrow="Import workspace" title="游戏入库" description="查看整个入库流水线的健康状态、待处理事项和最近结果。" actions={<><ButtonLink href="/admin/imports/tasks" secondary>任务进度</ButtonLink><ButtonLink href="/admin/imports/new">＋ 新建导入</ButtonLink></>} />
      <section className="kpi-grid">
        <Kpi label="运行中任务" value={summary.running} note="包含排队、处理与有界取消" tone="purple" />
        <Kpi label="待审核条目" value={summary.reviewPending} note="逐条确认候选与兼容性" tone="amber" />
        <Kpi label="异常任务" value={summary.failed} note="查看稳定错误码与可重试状态" tone="slate" />
        <Kpi label="已完成任务" value={summary.completed} note="全部批次的完成数量" tone="cyan" />
      </section>
      <section className="panel">
        <div className="panel-head"><div><h2>入库流水线</h2><p>每个阶段都有独立状态与可追踪证据</p></div><StatusBadge tone="good">系统就绪</StatusBadge></div>
        <div className="pipeline">
          {[['1','上传与去重'],['2','识别文件'],['3','兼容性验证'],['4','元信息候选'],['5','人工审核']].map(([number,label]) => <div className="pipeline-step" key={number}><i>{number}</i><strong>{label}</strong><span>等待新内容</span></div>)}
        </div>
      </section>
      <div className="split-grid" style={{ marginTop: 20 }}>
        <section className="panel"><div className="panel-head"><div><h2>需要处理</h2><p>阻塞范围较大的事项优先显示</p></div></div><div className="panel-body"><div className="metric-line"><span>待审核条目</span><strong>{summary.reviewPending}</strong></div><div className="metric-line"><span>异常任务</span><strong>{summary.failed}</strong></div></div></section>
        <section className="panel"><div className="panel-head"><div><h2>快速开始</h2><p>文件与目录均从浏览器选择</p></div></div><div className="panel-body"><p>上传先进入独立会话，完成逐块校验与 CAS 物化后才能创建 ImportJob。</p><ButtonLink href="/admin/imports/new">选择内容</ButtonLink></div></section>
      </div>
    </>
  );
}
