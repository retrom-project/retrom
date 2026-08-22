"use client";

import Link from "next/link";
import { useMemo, useState, type CSSProperties } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { formatBytes } from "@/lib/backend";
import { buildArcadeDependencyRows, type ArcadeDependencies, type ArcadeDependencyNode, type ArcadeParentAttachment } from "./arcade-dependency-tree";

const stateLabels: Record<ArcadeDependencyNode["state"], string> = {
  MISSING: "缺失",
  MISMATCH: "内容不匹配",
  SATISFIED_EXTERNAL: "已匹配",
  SATISFIED_BY_CONTENT: "由游戏文件满足",
  HASH_WARNING: "已匹配（有校验警告）",
};

const progressLabels: Record<string, string> = {
  snapshot: "读取任务状态",
  queued: "等待校验",
  archive_scanned: "ZIP 安全扫描完成",
  parent_matched: "Parent 内容已匹配",
  parent_rejected: "Parent 内容不匹配",
  source_snapshot_created: "来源快照已创建",
  core_validation_completed: "运行检查已完成",
  succeeded: "校验完成",
  failed: "校验失败",
  cancelled: "校验已取消",
};

function attachmentSummary(attachment: ArcadeParentAttachment | null) {
  if (!attachment) {return null;}
  if (attachment.state === "QUEUED" || attachment.state === "RUNNING") {return "正在校验…";}
  if (attachment.state === "FAILED_RETRYABLE") {return "校验服务暂时不可用，可重试";}
  if (attachment.state === "REJECTED") {
    const missing = attachment.diagnostics?.missingEntries?.slice(0, 3) ?? [];
    const mismatched = attachment.diagnostics?.mismatchedEntries?.slice(0, 3) ?? [];
    const details = [...missing, ...mismatched];
    return details.length ? `上次文件不匹配：${details.join("、")}` : `上次文件未通过：${attachment.errorCode ?? "内容不匹配"}`;
  }
  if (attachment.state === "CANCELLED") {return "上次校验已取消";}
  return null;
}

export function ArcadeDependencyCard({
  value,
  disabled,
  progress,
  onAttach,
  onRetry,
}: {
  value: ArcadeDependencies;
  disabled: boolean;
  progress: string;
  onAttach: (node: ArcadeDependencyNode, file: File) => Promise<boolean>;
  onRetry: (attachment: ArcadeParentAttachment) => Promise<void>;
}) {
  const rows = useMemo(() => buildArcadeDependencyRows(value), [value]);
  const dependencyStatusTone = rows.length === 0 ? "info" : value.status === "READY" ? "good" : "warn";
  const dependencyStatusLabel = rows.length === 0 ? "无额外依赖" : value.status === "READY" ? "全部满足" : "需要处理";
  const [target, setTarget] = useState<ArcadeDependencyNode | null>(null);
  const [file, setFile] = useState<File | null>(null);
  const [fileError, setFileError] = useState("");

  const close = () => { setTarget(null); setFile(null); setFileError(""); };
  const submit = async () => {
    if (!target || !file) { setFileError("请选择一个 ZIP 文件"); return; }
    if (!file.name.toLowerCase().endsWith(".zip")) { setFileError("Parent ROM 必须是 ZIP 文件"); return; }
    if (await onAttach(target, file)) {close();}
  };

  return <section className="panel arcade-dependency-card" aria-labelledby="arcade-dependency-title">
    <div className="panel-head"><div><h2 id="arcade-dependency-title">Arcade 运行依赖</h2><p>依赖关系来自锁定 DAT；Parent ZIP 按内容识别，本地文件名可以不同。</p></div><span className={`status ${dependencyStatusTone}`}><i />{dependencyStatusLabel}</span></div>
    <div className="panel-body">
      <div className="arcade-dependency-root"><strong title={`${value.machine}.zip`}>{value.machine}.zip</strong><span>游戏内容</span></div>
      {rows.length ? <ol className="arcade-dependency-tree">{rows.map(({ node, level }) => <ArcadeDependencyRow
        key={`${node.kind}-${node.machine}`}
        {...{ disabled, level, node, onRetry }}
        rootMachine={value.machine}
        onAttach={() => setTarget(node)}
      />)}</ol> : <p className="arcade-dependency-empty">当前没有额外 Parent 或 BIOS/Base 依赖。</p>}
      {progress ? <p className="arcade-dependency-progress" role="status" aria-live="polite"><i className="button-spinner" aria-hidden="true" />{progressLabels[progress] ?? progress}</p> : null}
    </div>
    <ParentAttachmentDialog {...{
      close, disabled, file, fileError, progress, setFile, setFileError, submit, target,
    }} rootMachine={value.machine} />
  </section>;
}

function ArcadeDependencyRow({ disabled, level, node, onAttach, onRetry, rootMachine }: {
  disabled: boolean;
  level: number;
  node: ArcadeDependencyNode;
  onAttach: () => void;
  onRetry: (attachment: ArcadeParentAttachment) => Promise<void>;
  rootMachine: string;
}) {
  const attachment = node.attachment;
  const active = attachment?.state === "QUEUED" || attachment?.state === "RUNNING";
  const needsAction = node.state === "MISSING" || node.state === "MISMATCH";
  return <li style={{ "--dependency-level": level } as CSSProperties}>
    <span className="arcade-dependency-branch" aria-hidden="true">└─</span>
    <DependencyCopy node={node} rootMachine={rootMachine} />
    <span className={`arcade-dependency-state ${needsAction ? "needs-action" : "satisfied"}`}>{active ? "正在校验…" : stateLabels[node.state]}</span>
    <DependencyAction {...{ active, attachment, disabled, node, onAttach, onRetry }} />
  </li>;
}

function DependencyCopy({ node, rootMachine }: { node: ArcadeDependencyNode; rootMachine: string }) {
  const summary = attachmentSummary(node.attachment);
  const kind = node.kind === "PARENT" ? "Parent" : "BIOS/Base";
  return <div className="arcade-dependency-copy"><strong title={node.expectedLogicalName}>{node.expectedLogicalName}</strong><span>{kind} · 由 {node.requiredBy ?? rootMachine}.zip 需要 · {node.requiredEntryCount} 个必需项</span>{summary ? <small>{summary}</small> : null}</div>;
}

function DependencyAction({ active, attachment, disabled, node, onAttach, onRetry }: {
  active: boolean;
  attachment: ArcadeParentAttachment | null;
  disabled: boolean;
  node: ArcadeDependencyNode;
  onAttach: () => void;
  onRetry: (attachment: ArcadeParentAttachment) => Promise<void>;
}) {
  if (node.kind === "BIOS_OR_BASE" && node.state === "MISSING") {
    return <div className="arcade-dependency-action"><Link className="button secondary compact" href={node.managementUrl ?? "/admin/bios"}>前往 BIOS 文件</Link></div>;
  }
  if (attachment?.state === "FAILED_RETRYABLE") {
    return <div className="arcade-dependency-action"><button type="button" className="button secondary compact" disabled={disabled} onClick={() => void onRetry(attachment)}>重试校验</button></div>;
  }
  if (!node.canAttach) {return <div className="arcade-dependency-action" />;}
  const label = node.state === "MISMATCH" || attachment?.state === "REJECTED"
    ? "重新上传 Parent ROM"
    : "补充 Parent ROM";
  return <div className="arcade-dependency-action"><button type="button" className="button secondary compact" disabled={disabled || active} onClick={onAttach}>{label}</button></div>;
}

function ParentAttachmentDialog({
  close, disabled, file, fileError, progress, rootMachine, setFile, setFileError, submit, target,
}: {
  close: () => void;
  disabled: boolean;
  file: File | null;
  fileError: string;
  progress: string;
  rootMachine: string;
  setFile: (file: File | null) => void;
  setFileError: (error: string) => void;
  submit: () => Promise<void>;
  target: ArcadeDependencyNode | null;
}) {
  const selectFile = (selected: File | null) => {
    setFile(selected);
    setFileError(selected && !selected.name.toLowerCase().endsWith(".zip") ? "Parent ROM 必须是 ZIP 文件" : "");
  };
  return <ConfirmDialog open={target !== null} title={`补充 ${target?.expectedLogicalName ?? "Parent ROM"}`} description={`这是 ${target?.requiredBy ?? rootMachine}.zip 的 Parent ROM。请选择对应 ROMset ZIP；系统按内容校验。`} confirmLabel="开始上传并校验" busy={disabled} onCancel={close} onConfirm={() => void submit()}>
    <label className="arcade-parent-file">选择一个 ZIP<input type="file" accept=".zip,application/zip" disabled={disabled} onChange={(event) => selectFile(event.target.files?.[0] ?? null)} /></label>
    {file ? <p className="arcade-parent-selection"><strong>{file.name}</strong><span>{formatBytes(file.size)}</span></p> : null}
    {fileError ? <p className="field-error" role="alert">{fileError}</p> : null}
    {progress ? <p className="arcade-dependency-progress" role="status" aria-live="polite"><i className="button-spinner" aria-hidden="true" />{progressLabels[progress] ?? progress}</p> : null}
  </ConfirmDialog>;
}
