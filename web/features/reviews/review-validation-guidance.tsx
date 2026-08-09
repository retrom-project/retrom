import Link from "next/link";
import { FeedbackBanner } from "@/components/ui";

export type ReviewDependencySnapshot = {
  bios?: Array<{
    logicalName?: string;
    requirementMode?: string;
    blobId?: string | null;
    installationStatus?: string | null;
  }>;
  dependencies?: Array<{
    kind?: string;
    machine?: string;
    state?: string;
    requiredEntries?: string[];
  }>;
  missingEntries?: string[];
  mismatchedEntries?: string[];
  warnings?: string[];
};

const compatibilityLabels: Record<string, string> = {
  READY: "运行检查已通过",
  LAUNCH_BIOS_MISSING: "缺少必需 BIOS 文件",
  LAUNCH_PARENT_MISSING: "缺少街机父级或依赖文件",
  ARCADE_DAT_UNAVAILABLE: "街机数据目录不可用",
  ARCADE_CONTENT_MISSING_ENTRY: "街机 ROM 集缺少文件",
  ARCADE_DEPENDENCY_MISMATCH: "街机依赖文件不匹配",
  UNSUPPORTED_CONTENT_FORMAT: "当前运行方式不支持这个文件",
  NEEDS_VALIDATION: "运行检查尚未完成",
  PENDING: "运行检查尚未完成",
};

export function reviewCompatibilityLabel(code: string, status: string) {
  if (status === "READY") return compatibilityLabels.READY;
  return compatibilityLabels[code] ?? "运行检查未通过";
}

export function ReviewValidationGuidance({ status, compatibilityCode, snapshot }: {
  status: string;
  compatibilityCode: string;
  snapshot?: ReviewDependencySnapshot;
}) {
  if (status === "READY") return null;
  const missingBIOS = (snapshot?.bios ?? []).filter((item) => item.requirementMode !== "OPTIONAL" && !item.blobId);
  const missingEntries = snapshot?.missingEntries ?? [];
  const mismatchedEntries = snapshot?.mismatchedEntries ?? [];
  const title = reviewCompatibilityLabel(compatibilityCode, status);

  if (compatibilityCode === "LAUNCH_BIOS_MISSING") {
    const query = missingBIOS[0]?.logicalName;
    const href = `/admin/bios?scope=FULL_CATALOG&status=MISSING${query ? `&q=${encodeURIComponent(query)}` : ""}`;
    return <FeedbackBanner tone="bad"><div className="review-validation-guidance"><strong>{title}</strong><p>发布已暂停。安装下面的必需文件后返回本页，点击“重新运行检查”，无需重新导入游戏。</p>{missingBIOS.length ? <ul>{missingBIOS.map((item, index) => <li key={`${item.logicalName ?? "BIOS"}-${index}`}><code>{item.logicalName ?? "未命名 BIOS"}</code></li>)}</ul> : null}<Link className="button secondary compact" href={href}>前往 BIOS 文件安装</Link></div></FeedbackBanner>;
  }

  if (compatibilityCode === "ARCADE_DAT_UNAVAILABLE") {
    return <FeedbackBanner tone="bad"><div className="review-validation-guidance"><strong>{title}</strong><p>请先准备并启用与当前街机运行方式匹配的数据目录，然后返回本页重新运行检查。</p><Link className="button secondary compact" href="/admin/bios/dats">前往街机数据目录</Link></div></FeedbackBanner>;
  }

  return <FeedbackBanner tone="bad"><div className="review-validation-guidance"><strong>{title}</strong><p>{status === "PENDING" || compatibilityCode === "NEEDS_VALIDATION" ? "点击“重新运行检查”获取最新结论。" : "发布已暂停。修正下列运行依赖后重新运行检查；如果目录选择错误，也可以返回任务进度重新配置。"}</p>{missingEntries.length || mismatchedEntries.length ? <ul>{missingEntries.map((entry) => <li key={`missing-${entry}`}><code>{entry}</code> 缺失</li>)}{mismatchedEntries.map((entry) => <li key={`mismatch-${entry}`}><code>{entry}</code> 不匹配</li>)}</ul> : <code>{compatibilityCode || status}</code>}</div></FeedbackBanner>;
}
