import Link from "next/link";
import { FeedbackBanner } from "@/components/ui";

export type ReviewDependencySnapshot = {
  bios?: Array<{ logicalName?: string; requirementMode?: string; fileRecord?: string | null; installationStatus?: string | null }>;
  dependencies?: Array<{ kind?: string; machine?: string; state?: string; requiredEntries?: string[] }>;
  missingEntries?: string[];
  mismatchedEntries?: string[];
  warnings?: string[];
  externalRTP?: Array<{ slot?: number; declaredName?: string }>;
};

const compatibilityLabels: Record<string, string> = {
  READY: "运行检查已通过",
  RPG_EXTERNAL_RTP_REQUIRED: "项目声明了外部 RTP",
  LAUNCH_BIOS_MISSING: "缺少必需 BIOS 文件",
  LAUNCH_PARENT_MISSING: "缺少街机父级或依赖文件",
  ARCADE_DAT_UNAVAILABLE: "街机数据目录不可用",
  ARCADE_CONTENT_MISSING_ENTRY: "街机 ROM 集缺少文件",
  ARCADE_DEPENDENCY_MISMATCH: "街机依赖文件不匹配",
  UNSUPPORTED_CONTENT_FORMAT: "当前运行方式不支持这个文件",
  NEEDS_VALIDATION: "运行检查尚未完成",
  PENDING: "运行检查尚未完成",
  TRIAL_REQUIRED: "需要试运行确认",
};

export function reviewCompatibilityLabel(code: string, status: string) {
  if (status === "READY") {return compatibilityLabels.READY;}
  return compatibilityLabels[code] ?? "运行检查未通过";
}

export function ReviewValidationGuidance({ status, compatibilityCode, snapshot, screenshotApproval = false }: {
  status: string;
  compatibilityCode: string;
  snapshot?: ReviewDependencySnapshot;
  screenshotApproval?: boolean;
}) {
  if (status === "READY") {return null;}
  const missingBIOS = (snapshot?.bios ?? []).filter((item) => item.requirementMode !== "OPTIONAL" && !item.fileRecord);
  const missingArchives = (snapshot?.dependencies ?? [])
    .filter((item) => item.state === "MISSING" && item.machine)
    .map((item) => `${item.machine}.zip`);
  const missingEntries = [...new Set([...(snapshot?.missingEntries ?? []), ...missingArchives])];
  const mismatchedEntries = snapshot?.mismatchedEntries ?? [];
  const logicalNames = [...new Set([...missingEntries, ...missingBIOS.flatMap((item) => item.logicalName ? [item.logicalName] : [])])];
  const scrollable = logicalNames.length + mismatchedEntries.length > 8;
  return <FeedbackBanner tone={screenshotApproval ? "info" : "bad"} marker={false}>
    <div className="review-validation-guidance" tabIndex={scrollable ? 0 : undefined} role={scrollable ? "region" : undefined} aria-label={scrollable ? "运行检查错误详情，可滚动查看" : undefined}>
      <strong>{reviewCompatibilityLabel(compatibilityCode, status)}</strong>
      <p>{screenshotApproval
        ? "已保存运行截图，可由管理员确认发布。以下检查原因仍保留供审核参考。"
        : "可修正以下问题，或试运行确认可以游玩并保存截图，再由管理员确认发布。"}</p>
      <code>{compatibilityCode || status}</code>
      <BlockerRemediation compatibilityCode={compatibilityCode} logicalNames={logicalNames} />
      {logicalNames.length || mismatchedEntries.length ? <ul>
        {logicalNames.map((entry) => <li key={`missing-${entry}`}><code>{entry}</code> 缺失</li>)}
        {mismatchedEntries.map((entry) => <li key={`mismatch-${entry}`}><code>{entry}</code> 不匹配</li>)}
      </ul> : null}
      {snapshot?.externalRTP?.length ? <ul>{snapshot.externalRTP.map((entry, index) => <li key={`${entry.slot}-${index}`}>{entry.declaredName || `RTP ${entry.slot ?? index + 1}`}</li>)}</ul> : null}
      {snapshot?.warnings?.length ? <ul>{snapshot.warnings.map((warning, index) => <li key={`${index}-${warning}`}>{warning}</li>)}</ul> : null}
    </div>
  </FeedbackBanner>;
}

function BlockerRemediation({ compatibilityCode, logicalNames }: { compatibilityCode: string; logicalNames: string[] }) {
  if (compatibilityCode === "RPG_EXTERNAL_RTP_REQUIRED") {
    return <p>可补齐游戏素材后重新导入；如果项目可以独立运行，也可在项目检查中勾选“确认项目自包含 RTP”。</p>;
  }
  if (compatibilityCode === "ARCADE_DAT_UNAVAILABLE") {
    return <><p>当前核心固定的内置 Arcade DAT 尚未准备完成。请检查服务的依赖准备和 Ready 状态；恢复后刷新本页查看当前检查结果。</p><code>make prepare-deps</code></>;
  }
  if (compatibilityCode === "LAUNCH_BIOS_MISSING") {
    const suffix = logicalNames[0] ? `&q=${encodeURIComponent(logicalNames[0])}` : "";
    return <><p>安装所列必需文件或街机依赖包后返回并刷新本页，无需重新导入游戏。</p><Link className="button secondary compact" href={`/admin/bios?scope=FULL_CATALOG&status=MISSING${suffix}`}>安装所需 BIOS 文件</Link></>;
  }
  return null;
}
