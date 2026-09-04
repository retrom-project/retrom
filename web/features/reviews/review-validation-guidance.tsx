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
  if (status === "READY") {return compatibilityLabels.READY;}
  return compatibilityLabels[code] ?? "运行检查未通过";
}

export function ReviewValidationGuidance({ status, compatibilityCode, snapshot }: {
  status: string;
  compatibilityCode: string;
  snapshot?: ReviewDependencySnapshot;
}) {
  if (status === "READY") {return null;}
  const missingBIOS = (snapshot?.bios ?? []).filter((item) => item.requirementMode !== "OPTIONAL" && !item.blobId);
  const missingEntries = snapshot?.missingEntries ?? [];
  const mismatchedEntries = snapshot?.mismatchedEntries ?? [];
  const missingArcadeArchives = (snapshot?.dependencies ?? [])
    .filter((item) => item.kind === "BIOS_OR_BASE" && item.state === "MISSING" && item.machine)
    .map((item) => `${item.machine}.zip`);
  const title = reviewCompatibilityLabel(compatibilityCode, status);

  if (compatibilityCode === "LAUNCH_BIOS_MISSING") {
    return <MissingBIOSGuidance {...{ compatibilityCode, missingArcadeArchives, missingBIOS, missingEntries, title }} />;
  }

  if (compatibilityCode === "ARCADE_DAT_UNAVAILABLE") {
    return <FeedbackBanner tone="bad" marker={false}><div className="review-validation-guidance"><strong>{title}</strong><p>当前核心固定的内置 Arcade DAT 尚未准备完成。请检查服务的依赖准备和 Ready 状态；恢复后刷新本页查看当前检查结果。</p><code>make prepare-deps</code></div></FeedbackBanner>;
  }

  const scrollable = missingEntries.length + mismatchedEntries.length > 8;
  return <DependencyGuidance {...{ compatibilityCode, mismatchedEntries, missingEntries, scrollable, status, title }} />;
}

function MissingBIOSGuidance({ compatibilityCode, missingArcadeArchives, missingBIOS, missingEntries, title }: {
  compatibilityCode: string;
  missingArcadeArchives: string[];
  missingBIOS: NonNullable<ReviewDependencySnapshot["bios"]>;
  missingEntries: string[];
  title: string;
}) {
  const logicalNames = [...new Set([
    ...missingBIOS.map((item) => item.logicalName).filter((item): item is string => Boolean(item)),
    ...missingArcadeArchives,
    ...missingEntries.filter((entry) => entry.toLocaleLowerCase("en-US").endsWith(".zip")),
  ])];
  const scrollable = logicalNames.length > 8;
  const query = logicalNames[0];
  const suffix = query ? `&q=${encodeURIComponent(query)}` : "";
  const href = `/admin/bios?scope=FULL_CATALOG&status=MISSING${suffix}`;
  return <FeedbackBanner tone="bad" marker={false}><div className="review-validation-guidance" tabIndex={scrollable ? 0 : undefined} role={scrollable ? "region" : undefined} aria-label={scrollable ? "运行检查错误详情，可滚动查看" : undefined}><strong>{title}</strong><p>发布已暂停。安装下面准确列出的必需文件或街机依赖包后返回并刷新本页，无需重新导入游戏。</p>{logicalNames.length ? <ul>{logicalNames.map((logicalName) => <li key={logicalName}><code>{logicalName}</code></li>)}</ul> : <code>{compatibilityCode}</code>}<Link className="button secondary compact" href={href}>安装所需 BIOS 文件</Link></div></FeedbackBanner>;
}

function DependencyGuidance({ compatibilityCode, mismatchedEntries, missingEntries, scrollable, status, title }: {
  compatibilityCode: string;
  mismatchedEntries: string[];
  missingEntries: string[];
  scrollable: boolean;
  status: string;
  title: string;
}) {
  const pending = status === "PENDING" || compatibilityCode === "NEEDS_VALIDATION";
  const hasEntries = missingEntries.length > 0 || mismatchedEntries.length > 0;
  return <FeedbackBanner tone="bad" marker={false}><div className="review-validation-guidance" tabIndex={scrollable ? 0 : undefined} role={scrollable ? "region" : undefined} aria-label={scrollable ? "运行检查错误详情，可滚动查看" : undefined}><strong>{title}</strong><p>{pending ? "刷新页面获取当前检查结论。" : "发布已暂停。修正下列运行依赖后刷新页面；如果目录选择错误，也可以返回任务进度重新配置。"}</p>{hasEntries ? <ul>{missingEntries.map((entry) => <li key={`missing-${entry}`}><code>{entry}</code> 缺失</li>)}{mismatchedEntries.map((entry) => <li key={`mismatch-${entry}`}><code>{entry}</code> 不匹配</li>)}</ul> : <code>{compatibilityCode || status}</code>}</div></FeedbackBanner>;
}
