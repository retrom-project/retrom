import Link from "next/link";
import type { ReviewApprovalSummary } from "./review-approval";
import { MissingBiosList } from "./missing-bios-list";

export function ReviewApprovalStatus({ busy, summary }: { busy: boolean; summary: ReviewApprovalSummary | null }) {
  if (!busy && !summary) { return null; }
  return <section className="panel review-approval-status" aria-label="快速审批进度">
    <div role="status" aria-live="polite">
      <h2>{approvalTitle(busy, summary)}</h2>
      <p>{summary ? `已检查 ${summary.checked} / ${summary.total} 项 · 已发布 ${summary.approved} 项 · 缺少 BIOS ${summary.missingBios} 项 · 失败 ${summary.failed} 项` : "正在读取当前筛选结果中的全部待审游戏…"}</p>
    </div>
    {summary?.interrupted ? <p>未处理的条目继续待审，请恢复访问后重试。</p> : null}
    {summary && summary.missingBios > 0 ? <MissingGames summary={summary} /> : null}
    {summary && summary.failed > 0 ? <details>
      <summary>查看失败条目{summary.failed > 20 ? "（前 20 项）" : ""}</summary>
      <ul>{summary.failures.slice(0, 20).map(({ game, message }) => <li key={game.id}>
        <Link href={`/admin/reviews/${game.id}`}>{game.title}</Link><span>{message}</span>
      </li>)}</ul>
    </details> : null}
    {!busy && summary && (summary.missingBios > 0 || summary.failed > 0) ? <p>这些条目继续待审；补齐 BIOS 或修正问题后，再次快速审批即可重新检查。</p> : null}
  </section>;
}

function approvalTitle(busy: boolean, summary: ReviewApprovalSummary | null) {
  if (busy) { return "正在快速审批"; }
  return summary?.interrupted ? "快速审批已停止" : "快速审批已完成";
}

function MissingGames({ summary }: { summary: ReviewApprovalSummary }) {
  return <details>
      <summary>缺少 BIOS 的游戏（{summary.missingBios} 款{summary.missingBios > 20 ? "，仅列前 20 款" : ""}）</summary>
      <ul>{summary.missingBiosDetails.slice(0, 20).map(({ game, requirements }) => <li key={game.id}>
        <Link href={`/admin/reviews/${game.id}`}>{game.title}</Link>
        <MissingBiosList items={requirements} />
      </li>)}</ul>
      <Link className="button secondary" href="/admin/bios">管理运行依赖</Link>
  </details>;
}
