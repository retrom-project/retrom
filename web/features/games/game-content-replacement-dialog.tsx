"use client";

import { useEffect, useMemo, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { FeedbackBanner } from "@/components/ui";
import { MultiDiscModeField } from "@/features/imports/multidisc-mode-field";
import { preflightMultiDisc, type MultiDiscPreflight as MultiDiscPreflightResult } from "@/features/imports/multidisc-preflight";
import { MultiDiscPreflight } from "@/features/imports/multidisc-preflight-view";
import { formatBytes } from "@/lib/backend";

type ContentMode = "STANDARD" | "MULTI_DISC" | "RPG_MAKER_PROJECT";

export function GameContentReplacementDialog({
  initialMode,
  multiDiscLimits,
  disabled,
  saveStateCount,
  onSubmit,
}: {
  initialMode: ContentMode;
  multiDiscLimits: { maxDiscs: number; maxTotalBytes: number } | null;
  disabled: boolean;
  saveStateCount: number;
  onSubmit: (files: File[], mode: ContentMode) => Promise<boolean>;
}) {
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<ContentMode>(
    initialMode === "RPG_MAKER_PROJECT" || initialMode === "MULTI_DISC" && multiDiscLimits
      ? initialMode
      : "STANDARD",
  );
  const [files, setFiles] = useState<File[]>([]);
  const [preflight, setPreflight] = useState<MultiDiscPreflightResult | null>(null);
  const totalBytes = useMemo(() => files.reduce((total, file) => total + file.size, 0), [files]);
  const preflighting = mode === "MULTI_DISC" && files.length > 0 && preflight === null;

  useEffect(() => {
    if (!open || mode !== "MULTI_DISC" || !multiDiscLimits || !files.length) {return;}
    let disposed = false;
    void preflightMultiDisc(files.map((file) => ({ path: file.webkitRelativePath || file.name, file })), multiDiscLimits)
      .then((result) => { if (!disposed) {setPreflight(result);} });
    return () => { disposed = true; };
  }, [files, mode, multiDiscLimits, open]);

  const close = () => {
    if (disabled) {return;}
    setOpen(false);
    setFiles([]);
    setPreflight(null);
  };
  const multiDiscValid = mode !== "MULTI_DISC" || Boolean(
    preflight?.detected && preflight.groups.length === 1 && preflight.completeGroupCount === 1 &&
    preflight.blockedGroupCount === 0 && preflight.rejectedGroupCount === 0,
  );
  const submit = async () => {
    if (!files.length || preflighting || !multiDiscValid) {return;}
    if (await onSubmit(files, mode)) {close();}
  };

  return <>
    <button className="button secondary" type="button" disabled={disabled} onClick={() => setOpen(true)}>替换游戏文件</button>
    <ConfirmDialog open={open} wide title="替换游戏内容" description={`只有新内容通过校验才会切换。当前 ${saveStateCount} 份存档会继续保留；内容完全相同时不会替换。`} confirmLabel="上传并替换内容" busy={disabled} confirmDisabled={!files.length || preflighting || !multiDiscValid} onCancel={close} onConfirm={() => void submit()}>
      <ReplacementDialogContents {...{
        disabled, files, mode, multiDiscLimits, preflight, preflighting, setFiles, setMode, setPreflight, totalBytes,
      }} />
    </ConfirmDialog>
  </>;
}

function ReplacementDialogContents({
  disabled, files, mode, multiDiscLimits, preflight, preflighting, setFiles, setMode, setPreflight, totalBytes,
}: {
  disabled: boolean;
  files: File[];
  mode: ContentMode;
  multiDiscLimits: { maxDiscs: number; maxTotalBytes: number } | null;
  preflight: MultiDiscPreflightResult | null;
  preflighting: boolean;
  setFiles: (files: File[]) => void;
  setMode: (mode: ContentMode) => void;
  setPreflight: (preflight: MultiDiscPreflightResult | null) => void;
  totalBytes: number;
}) {
  const multiDisc = mode === "MULTI_DISC";
  const rpgMaker = mode === "RPG_MAKER_PROJECT";
  const changeMode = (selected: boolean) => {
    setMode(selected ? "MULTI_DISC" : "STANDARD");
    setFiles([]);
    setPreflight(null);
  };
  const selectFiles = (selected: File[]) => {
    setFiles(selected);
    setPreflight(null);
  };
  return <div className="game-content-replacement">
    {rpgMaker ? <FeedbackBanner tone="info">请选择完整的 RPG Maker 项目目录。服务端会重新识别世代；与当前游戏世代不同的项目会被拒绝，当前内容与存档保持不变。</FeedbackBanner> : multiDiscLimits ? <MultiDiscModeField selected={multiDisc} detectedGroupCount={preflight?.processableGroupCount ?? 0} maxDiscs={multiDiscLimits.maxDiscs} maxTotalBytes={multiDiscLimits.maxTotalBytes} onChange={changeMode} /> : <FeedbackBanner tone="info">当前游戏目录只允许替换普通内容；既有多盘内容仍可继续读取和启动。</FeedbackBanner>}
    <label className="game-content-replacement-picker">{rpgMaker ? "选择同世代 RPG Maker 项目目录" : multiDisc ? "选择一份完整多盘目录" : "选择新的游戏文件"}<input aria-label={rpgMaker ? "选择同世代 RPG Maker 项目目录" : multiDisc ? "选择一份完整多盘目录" : "选择新的游戏文件"} type="file" multiple disabled={disabled} {...(multiDisc || rpgMaker ? { webkitdirectory: "" } : {})} onChange={(event) => selectFiles(Array.from(event.currentTarget.files ?? []))} /></label>
    {files.length ? <div className="game-content-replacement-summary"><span>已选择 {files.length} 个文件</span><strong>{formatBytes(totalBytes)}</strong></div> : null}
    {preflighting ? <p className="game-content-replacement-progress" role="status"><i className="button-spinner" aria-hidden="true" />正在预检多盘目录…</p> : null}
    <ReplacementPreflightFeedback {...{ files, multiDisc, preflight, preflighting }} />
  </div>;
}

function ReplacementPreflightFeedback({ files, multiDisc, preflight, preflighting }: {
  files: File[];
  multiDisc: boolean;
  preflight: MultiDiscPreflightResult | null;
  preflighting: boolean;
}) {
  if (!multiDisc || preflighting || !preflight) {return null;}
  if (files.length > 0 && !preflight.detected) {
    return <FeedbackBanner tone="bad">所选目录中没有 M3U 播放列表。</FeedbackBanner>;
  }
  if (!preflight.detected) {return null;}
  return <><MultiDiscPreflight result={preflight} allowIncomplete={false} />{preflight.groups.length !== 1 ? <FeedbackBanner tone="bad">一次替换必须且只能包含一个多盘游戏目录。</FeedbackBanner> : null}</>;
}
