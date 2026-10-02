import type { SourceImportSummary } from "./source-import-model";

const explanations = {
  PENDING: "扫描还未完成", READY: "扫描完成", PARTIAL: "部分 metadata 被拒绝，合法集合可以继续",
  INVALID: "没有可映射的有效集合", EMPTY: "扫描范围没有游戏内容", NO_METADATA: "目录中没有所选格式的 metadata",
};

export function SourceScanDiagnostics({ summary }: { summary: SourceImportSummary }) {
  if (summary.scanOutcome === "PENDING" || summary.scanOutcome === "READY") {return null;}
  return <section className="source-scan-diagnostics panel" aria-label="扫描诊断">
    <h3>{explanations[summary.scanOutcome]}</h3>
    <p>{summary.counts.metadata} 个 metadata · {summary.counts.invalidMetadata} 个无效 · {summary.counts.collections} 个集合 · {summary.counts.games} 个游戏</p>
    <p>修正源文件后可以重新扫描，或返回修改目录和文件组织格式。</p>
    <ul>{summary.scanDiagnostics.map((diagnostic) => <li key={`${diagnostic.relativePath}-${diagnostic.code}`}>
      <strong>{diagnostic.relativePath}{diagnostic.line !== null ? ` · 第 ${diagnostic.line} 行` : ""}</strong>
      <p>{diagnostic.message}</p><code>{diagnostic.code}</code>
    </li>)}</ul>
    {summary.counts.invalidMetadata > summary.scanDiagnostics.length ? <p>显示前 {summary.scanDiagnostics.length} 条诊断，共 {summary.counts.invalidMetadata} 个无效文件。</p> : null}
  </section>;
}
