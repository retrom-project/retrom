import type { components } from "@/lib/api/generated/schema";
import { candidateFailureLabel } from "./candidate-diagnostics";

const checkLabels: Record<string, string> = { size: "大小", md5: "MD5", sha1: "SHA1", sha256: "SHA256" };
const resultLabels: Record<string, string> = { MATCHED: "匹配", MISMATCHED: "不匹配", NOT_CHECKED: "未提供校验依据" };

export function CandidateEvidence({ candidate }: { candidate: components["schemas"]["ServerBIOSImportCandidate"] }) {
  const evidence = candidate.evaluationDetails ?? {};
  if (evidence.code && !("matchedCount" in evidence) && !("checks" in evidence)) {
    return <p>{candidateFailureLabel(evidence.code)}；未完成内容匹配。</p>;
  }
  if ("matchedCount" in evidence) {
    return <p>DAT：匹配 {String(evidence.matchedCount ?? 0)} · 别名 {String(evidence.aliasedCount ?? 0)} · 不一致 {String(evidence.mismatchedCount ?? 0)} · 缺失 {String(evidence.missingCount ?? 0)} · 额外 {String(evidence.extraCount ?? 0)}</p>;
  }
  const checks = evidence.checks;
  if (checks && typeof checks === "object" && !Array.isArray(checks)) {
    return <p>{Object.entries(checks).filter(([name, result]) => checkLabels[name] && typeof result === "string" && resultLabels[result])
      .map(([name, result]) => `${checkLabels[name]}：${resultLabels[String(result)]}`).join(" · ")}</p>;
  }
  return <p>尚无内容匹配证据</p>;
}
