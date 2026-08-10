"use client";

import { useEffect, useMemo, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { FeedbackBanner } from "@/components/ui";
import { MultiDiscModeField } from "@/features/imports/multidisc-mode-field";
import { preflightMultiDisc, type MultiDiscPreflight as MultiDiscPreflightResult } from "@/features/imports/multidisc-preflight";
import { MultiDiscPreflight } from "@/features/imports/multidisc-preflight-view";
import { formatBytes } from "@/lib/backend";

type ContentMode = "STANDARD" | "MULTI_DISC_M3U_V1";

export function GameContentReplacementDialog({
  initialMode,
  multiDiscLimits,
  disabled,
  onSubmit,
}: {
  initialMode: ContentMode;
  multiDiscLimits: { maxDiscs: number; maxTotalBytes: number } | null;
  disabled: boolean;
  onSubmit: (files: File[], mode: ContentMode) => Promise<boolean>;
}) {
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<ContentMode>(initialMode === "MULTI_DISC_M3U_V1" && multiDiscLimits ? initialMode : "STANDARD");
  const [files, setFiles] = useState<File[]>([]);
  const [preflight, setPreflight] = useState<MultiDiscPreflightResult | null>(null);
  const totalBytes = useMemo(() => files.reduce((total, file) => total + file.size, 0), [files]);
  const preflighting = mode === "MULTI_DISC_M3U_V1" && files.length > 0 && preflight === null;

  useEffect(() => {
    if (!open || mode !== "MULTI_DISC_M3U_V1" || !multiDiscLimits || !files.length) return;
    let disposed = false;
    void preflightMultiDisc(files.map((file) => ({ path: file.webkitRelativePath || file.name, file })), multiDiscLimits)
      .then((result) => { if (!disposed) setPreflight(result); });
    return () => { disposed = true; };
  }, [files, mode, multiDiscLimits, open]);

  const close = () => {
    if (disabled) return;
    setOpen(false);
    setFiles([]);
    setPreflight(null);
  };
  const multiDiscValid = mode !== "MULTI_DISC_M3U_V1" || Boolean(
    preflight?.detected && preflight.groups.length === 1 && preflight.completeGroupCount === 1 &&
    preflight.blockedGroupCount === 0 && preflight.rejectedGroupCount === 0,
  );
  const submit = async () => {
    if (!files.length || preflighting || !multiDiscValid) return;
    if (await onSubmit(files, mode)) close();
  };

  return <>
    <button className="button secondary" type="button" disabled={disabled} onClick={() => setOpen(true)}>替换游戏文件</button>
    <ConfirmDialog open={open} wide title="替换游戏内容" description="新内容通过当前目录默认核心校验后才会成为当前版本；旧内容、历史版本和存档继续保留。" confirmLabel="上传并创建内容版本" busy={disabled} confirmDisabled={!files.length || preflighting || !multiDiscValid} onCancel={close} onConfirm={() => void submit()}>
      <div className="game-content-replacement">
        {multiDiscLimits ? <MultiDiscModeField selected={mode === "MULTI_DISC_M3U_V1"} detectedGroupCount={preflight?.processableGroupCount ?? 0} maxDiscs={multiDiscLimits.maxDiscs} maxTotalBytes={multiDiscLimits.maxTotalBytes} onChange={(selected) => { setMode(selected ? "MULTI_DISC_M3U_V1" : "STANDARD"); setFiles([]); setPreflight(null); }} /> : <FeedbackBanner tone="info">当前游戏目录只允许替换普通内容；既有多盘内容仍可继续读取和启动。</FeedbackBanner>}
        <label className="game-content-replacement-picker">{mode === "MULTI_DISC_M3U_V1" ? "选择一份完整多盘目录" : "选择新的游戏文件"}<input aria-label={mode === "MULTI_DISC_M3U_V1" ? "选择一份完整多盘目录" : "选择新的游戏文件"} type="file" multiple disabled={disabled} {...(mode === "MULTI_DISC_M3U_V1" ? { webkitdirectory: "" } : {})} onChange={(event) => { setFiles(Array.from(event.currentTarget.files ?? [])); setPreflight(null); }} /></label>
        {files.length ? <div className="game-content-replacement-summary"><span>已选择 {files.length} 个文件</span><strong>{formatBytes(totalBytes)}</strong></div> : null}
        {preflighting ? <p className="game-content-replacement-progress" role="status"><i className="button-spinner" aria-hidden="true" />正在预检多盘目录…</p> : null}
        {mode === "MULTI_DISC_M3U_V1" && files.length && !preflighting && preflight && !preflight.detected ? <FeedbackBanner tone="bad">所选目录中没有 M3U 播放列表。</FeedbackBanner> : null}
        {mode === "MULTI_DISC_M3U_V1" && preflight?.detected ? <MultiDiscPreflight result={preflight} allowIncomplete={false} /> : null}
        {mode === "MULTI_DISC_M3U_V1" && preflight?.detected && preflight.groups.length !== 1 ? <FeedbackBanner tone="bad">一次替换必须且只能包含一个多盘游戏目录。</FeedbackBanner> : null}
      </div>
    </ConfirmDialog>
  </>;
}
