"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { newUuid } from "@/lib/crypto";
import { responseError, uploadFiles } from "@/lib/upload";
import { formatBytes } from "@/lib/backend";
import { writeHeaders } from "@/lib/api/client";
import type { ImportDetail } from "./import-workflow";
import { MultiDiscModeField } from "./multidisc-mode-field";
import { TagChips, TagPicker, type TagReference } from "@/components/tag-picker";
import { MultiDiscPreflight as MultiDiscPreflightView } from "./multidisc-preflight-view";
import { MULTI_DISC_DEFAULT_LIMITS, preflightMultiDisc, type MultiDiscPreflight } from "./multidisc-preflight";
import { DirectoryPickerDialog } from "./directory-picker-dialog";
import { directoryPickerAvailable, droppedDirectory, pickDirectory, type PickedDirectory, type PickedDirectoryFile } from "@/lib/directory-access";

type ChosenFile = { id: string; file: File; name: string; size: number; path: string };
type ContentMode = "STANDARD" | "MULTI_DISC_M3U_V1" | "RPG_MAKER_PROJECT_V1" | "ONS_PROJECT_V1" |
  "KIRIKIRI_PROJECT_V1" | "BUTTERSCOTCH_PROJECT_V1";
type Directory = {
  id: string; name: string; platformName: string; coreName: string;
  importCapabilities?: { contentModes: string[]; multiDisc: { maxDiscs: number; maxTotalBytes: number } | null };
};

type ReusableFile = ImportDetail["fileOutcomes"][number];

function ImportStepper({ reconfiguring, step }: { reconfiguring: boolean; step: 1 | 2 | 3 }) {
  const labels = [[1, reconfiguring ? "复用内容" : "选择内容"], [2, "确认配置"], [3, reconfiguring ? "重新识别" : "上传并验证"]] as const;
  return <div className="stepper import-wizard-steps" aria-label="导入步骤">{labels.map(([number, label]) => <div className={`step${step === number ? " is-active" : step > number ? " is-complete" : ""}`} key={number}><i>{step > number ? "✓" : number}</i>{label}</div>)}</div>;
}

function FilePreview({ files }: { files: ChosenFile[] }) {
  return <section className="panel import-file-preview"><div className="panel-head"><div><h2>已选择文件</h2><p>当前展示前 20 项，共 {files.length} 个文件。</p></div></div><div className="table-wrap"><table><thead><tr><th>相对路径</th><th>类型</th><th>大小</th></tr></thead><tbody>{files.slice(0, 20).map((file) => <tr key={`${file.path}-${file.size}`}><td><strong>{file.path}</strong></td><td>{file.name.toLocaleLowerCase().endsWith(".zip") ? "ZIP 压缩包" : "游戏文件"}</td><td title={`${file.size} bytes`}>{formatBytes(file.size)}</td></tr>)}</tbody></table></div></section>;
}

function ReusableFilePreview({ files }: { files: ReusableFile[] }) {
  return <section className="panel import-file-preview"><div className="panel-head"><div><h2>将复用的文件</h2><p>当前展示前 20 项，共 {files.length} 个文件。</p></div></div><div className="table-wrap"><table><thead><tr><th>相对路径</th><th>原失败原因</th><th>大小</th></tr></thead><tbody>{files.slice(0, 20).map((file) => <tr key={file.uploadFileId}><td><strong>{file.name}</strong></td><td><code>{file.reasonCode ?? "REJECTED"}</code></td><td title={`${file.sizeBytes} bytes`}>{formatBytes(file.sizeBytes)}</td></tr>)}</tbody></table></div></section>;
}

function SelectedSourceSummary({ count, onNext, onReset, onToggleFiles, reconfiguring, showFiles, totalBytes }: {
  count: number;
  onNext: () => void;
  onReset?: () => void;
  onToggleFiles: () => void;
  reconfiguring: boolean;
  showFiles: boolean;
  totalBytes: number;
}) {
  return <div className="import-selected-summary"><div><span className="status good"><i />{reconfiguring ? "服务器文件可复用" : "已选择"}</span><h3>{count} 个文件 · {formatBytes(totalBytes)}</h3><p>{reconfiguring ? "原始失败记录会保留；新任务创建后，旧任务不再把这些文件计为待处理异常。" : "文件相对路径会完整保留，上传前仍可重新选择。"}</p></div><div><button className="button secondary" type="button" onClick={onToggleFiles}>{showFiles ? "收起文件清单" : "查看文件清单"}</button>{onReset ? <button className="button secondary" type="button" onClick={onReset}>重新选择</button> : null}<button className="button" type="button" onClick={onNext}>下一步</button></div></div>;
}

type SourceStepProps = {
  contentMode: ContentMode;
  files: ChosenFile[];
  onDrop: (files: FileList) => void;
  onNext: () => void;
  onPickDirectory: () => void;
  onPickFiles: () => void;
  onReset: () => void;
  onToggleFiles: () => void;
  preflight: MultiDiscPreflight | null;
  preflighting: boolean;
  reconfigureSource: ImportDetail | null;
  reusableFiles: ReusableFile[];
  showFiles: boolean;
  totalBytes: number;
};

function SourceDropZone({ contentMode, onDrop, onPickDirectory, onPickFiles }: Pick<SourceStepProps, "contentMode" | "onDrop" | "onPickDirectory" | "onPickFiles">) {
  const rpgMaker = contentMode === "RPG_MAKER_PROJECT_V1";
  const ons = contentMode === "ONS_PROJECT_V1";
  const kirikiri = contentMode === "KIRIKIRI_PROJECT_V1";
  const butterscotch = contentMode === "BUTTERSCOTCH_PROJECT_V1";
  const project = rpgMaker || ons || kirikiri || butterscotch;
  const projectName = rpgMaker ? "RPG Maker" : ons ? "ONS" : kirikiri ? "KiriKiri" : "GameMaker";
  return <div className="dropzone import-dropzone" onDragOver={(event) => event.preventDefault()} onDrop={(event) => { event.preventDefault(); onDrop(event.dataTransfer.files); }}><div><span aria-hidden="true">⇧</span><h2>{project ? `将 ${projectName} 项目归档或目录拖到这里` : "将游戏文件或目录拖到这里"}</h2><p>{project ? "只选择一个 ZIP/7z 项目归档，或选择完整项目目录；项目内相对路径会完整保留。" : "支持普通 ROM、Arcade ZIP、DOS 内容目录、多盘 M3U + CHD、RPG Maker 与 ONS 项目；相对路径会完整保留。"}</p><div className="dropzone-actions"><button className="button" type="button" onClick={onPickFiles}>{project ? "选择项目归档" : "选择文件"}</button><button className="button secondary" type="button" onClick={onPickDirectory}>{project ? "选择项目目录" : "选择目录"}</button></div></div></div>;
}

function ReconfigureDropZone({ count, onNext }: { count: number; onNext: () => void }) {
  return <div className="dropzone import-dropzone import-reuse-zone"><div><span aria-hidden="true">↻</span><h2>复用服务器中已上传的内容</h2><p>将重新处理原任务中尚未解决的 {count} 个文件，不会再次上传文件内容。</p><div className="dropzone-actions"><button className="button" type="button" disabled={!count} onClick={onNext}>重新选择平台目录</button><Link className="button secondary" href="/admin/imports/new">改为上传新文件</Link></div></div></div>;
}

function SourceStep(props: SourceStepProps) {
  return <section className="import-wizard-stage">
    {props.reconfigureSource ? <ReconfigureDropZone count={props.reusableFiles.length} onNext={props.onNext} /> : <SourceDropZone contentMode={props.contentMode} onDrop={props.onDrop} onPickDirectory={props.onPickDirectory} onPickFiles={props.onPickFiles} />}
    {props.preflighting ? <div className="feedback info" role="status">正在读取 M3U…</div> : props.preflight?.detected ? <MultiDiscPreflightView result={props.preflight} /> : null}
    {props.reconfigureSource && props.reusableFiles.length ? <><SelectedSourceSummary count={props.reusableFiles.length} onNext={props.onNext} onToggleFiles={props.onToggleFiles} reconfiguring showFiles={props.showFiles} totalBytes={props.totalBytes} />{props.showFiles ? <ReusableFilePreview files={props.reusableFiles} /> : null}</> : null}
    {props.files.length ? <><SelectedSourceSummary count={props.files.length} onNext={props.onNext} onReset={props.onReset} onToggleFiles={props.onToggleFiles} reconfiguring={false} showFiles={props.showFiles} totalBytes={props.totalBytes} />{props.showFiles ? <FilePreview files={props.files} /> : null}</> : null}
  </section>;
}

type ConfigStepProps = {
  activeTags: TagReference[];
  busy: boolean;
  contentMode: ContentMode;
  directories: Directory[];
  fileCount: number;
  multiDiscInvalid: boolean;
  projectInvalid: boolean;
  multiDiscLimits: { maxDiscs: number; maxTotalBytes: number };
  multiDiscSubmitLabel: string;
  multiDiscSupported: boolean;
  onBack: () => void;
  onContentMode: (selected: boolean) => void;
  onProvider: (value: string) => void;
  onSubmit: () => void;
  onTags: (tags: TagReference[]) => void;
  onTarget: (value: string) => void;
  preflight: MultiDiscPreflight | null;
  preflighting: boolean;
  provider: string;
  reconfiguring: boolean;
  selectedDirectory: Directory | undefined;
  tags: TagReference[];
  target: string;
  totalBytes: number;
  visibleCapabilityNotice: string;
};

function MultiDiscConfiguration(props: Pick<ConfigStepProps, "contentMode" | "multiDiscLimits" | "multiDiscSupported" | "onContentMode" | "preflight" | "reconfiguring" | "visibleCapabilityNotice"> & { sourceIsDirectory: boolean }) {
  const multiDisc = props.contentMode === "MULTI_DISC_M3U_V1";
  return <>
    {!props.reconfiguring && props.sourceIsDirectory && props.multiDiscSupported ? <MultiDiscModeField selected={multiDisc} detectedGroupCount={props.preflight?.detected ? props.preflight.processableGroupCount : 0} maxDiscs={props.multiDiscLimits.maxDiscs} maxTotalBytes={props.multiDiscLimits.maxTotalBytes} onChange={props.onContentMode} /> : null}
    {props.visibleCapabilityNotice ? <div className="feedback warn" role="alert">{props.visibleCapabilityNotice}</div> : null}
    {multiDisc && !props.preflight?.detected ? <div className="feedback bad" role="alert">所选目录树中必须至少包含一个 .m3u 文件</div> : null}
    {multiDisc && props.preflight?.detected ? <MultiDiscPreflightView result={props.preflight} /> : null}
  </>;
}

function ProjectConfiguration({ contentMode }: Pick<ConfigStepProps, "contentMode">) {
  if (contentMode === "RPG_MAKER_PROJECT_V1") {
    return <div className="feedback info" role="status">整个 RPG Maker 目录或单个 ZIP/7z 会作为一个项目导入；服务端会识别项目版本并选择底层核心。</div>;
  }
  if (contentMode === "ONS_PROJECT_V1") {
    return <div className="feedback info" role="status">整个 ONS 目录或单个 ZIP/7z 会作为一个项目导入；审核时需要先成功试运行一次。</div>;
  }
  if (contentMode === "KIRIKIRI_PROJECT_V1") {
    return <div className="feedback info" role="status">整个 KiriKiri 目录或单个 ZIP/7z 会作为一个项目导入；审核时需要先成功试运行一次。</div>;
  }
  if (contentMode === "BUTTERSCOTCH_PROJECT_V1") {
    return <div className="feedback info" role="status">整个 GameMaker 目录或单个 ZIP/7z 会作为一个项目导入；当前原型支持带 data.win 的项目，审核时需要先成功试运行一次。</div>;
  }
  return null;
}

function ImportConfigurationFields(props: Pick<ConfigStepProps, "contentMode" | "directories" | "onProvider" | "onTarget" | "provider" | "reconfiguring" | "selectedDirectory" | "target">) {
  return <div className="form-grid import-config-grid">
    <div className="field"><label htmlFor="directory">目标游戏目录</label><select id="directory" value={props.target} onChange={(event) => props.onTarget(event.target.value)}><option value="" disabled>{props.directories.length ? "请选择目标游戏目录" : "暂无可用游戏目录"}</option>{props.directories.map((directory) => <option value={directory.id} key={directory.id}>{directory.name}</option>)}</select><small>{props.reconfiguring ? "可以保留原目录，也可以选择正确的平台目录后重新识别。" : "必须主动选择，避免将游戏导入到错误目录。"}</small></div>
    <div className="field"><label htmlFor="provider">元信息来源</label>{isProjectContentMode(props.contentMode)
      ? <input id="provider" value={`不刮削（${contentModeLabel(props.contentMode)}）`} disabled />
      : <select id="provider" value={props.provider} onChange={(event) => props.onProvider(event.target.value)}><option value="HASHEOUS">Hasheous 哈希查询</option><option value="NONE">不刮削</option></select>}</div>
    <div className="field"><label>游戏平台</label><input value={props.selectedDirectory?.platformName ?? "选择目录后显示"} disabled /></div>
    <div className="field"><label>推荐运行方式</label><input value={props.selectedDirectory?.coreName ?? "选择目录后显示"} disabled /></div>
  </div>;
}

function ConfigStep(props: ConfigStepProps & { sourceIsDirectory: boolean }) {
  const submitLabel = props.reconfiguring
    ? "按新配置重新识别"
    : props.contentMode === "MULTI_DISC_M3U_V1" ? props.multiDiscSubmitLabel
      : props.contentMode === "RPG_MAKER_PROJECT_V1" ? "上传并验证 RPG Maker 项目"
        : props.contentMode === "ONS_PROJECT_V1" ? "上传并试运行 ONS 项目"
          : props.contentMode === "KIRIKIRI_PROJECT_V1" ? "上传并试运行 KiriKiri 项目"
            : props.contentMode === "BUTTERSCOTCH_PROJECT_V1" ? "上传并试运行 GameMaker 项目"
              : "开始上传并验证";
  return <section className="panel import-config-panel">
    <div className="panel-head"><div><h2>确认导入配置</h2><p>目标目录决定基础平台和推荐运行方式；配置会冻结到本次任务快照。</p></div><span className="status info"><i />步骤 2 / 3</span></div>
    <div className="panel-body">
      <ImportConfigurationFields contentMode={props.contentMode} directories={props.directories} onProvider={props.onProvider} onTarget={props.onTarget} provider={props.provider} reconfiguring={props.reconfiguring} selectedDirectory={props.selectedDirectory} target={props.target} />
      <div className="import-tag-config"><TagPicker label="批次默认标签" options={props.activeTags} selected={props.tags} onChange={props.onTags} disabled={props.busy} description="这些标签会冻结到任务配置，并作为每个待审核游戏的初始选择；审核时仍可逐项调整。" /></div>
      <MultiDiscConfiguration contentMode={props.contentMode} multiDiscLimits={props.multiDiscLimits} multiDiscSupported={props.multiDiscSupported} onContentMode={props.onContentMode} preflight={props.preflight} reconfiguring={props.reconfiguring} sourceIsDirectory={props.sourceIsDirectory} visibleCapabilityNotice={props.visibleCapabilityNotice} />
      <ProjectConfiguration contentMode={props.contentMode} />
      {props.projectInvalid ? <div className="feedback bad" role="alert">项目必须选择一个完整目录，或只选择一个 ZIP/7z 归档。</div> : null}
      <div className="import-config-summary"><div><small>内容</small><strong>{props.fileCount} 个文件</strong></div><div><small>数据量</small><strong>{formatBytes(props.totalBytes)}</strong></div><div><small>目标</small><strong>{props.selectedDirectory?.name ?? "尚未选择"}</strong></div><div><small>布局</small><strong>{contentModeLabel(props.contentMode)}</strong></div></div>
      {props.tags.length ? <div className="import-tag-summary"><small>将应用到待审核游戏</small><TagChips tags={props.tags} /></div> : null}
      <div className="import-stage-actions"><button className="button secondary" type="button" onClick={props.onBack}>上一步</button><button className="button" type="button" disabled={props.busy || props.preflighting || !props.target || props.multiDiscInvalid || props.projectInvalid} onClick={props.onSubmit}>{submitLabel}</button></div>
    </div>
  </section>;
}

function progressHeadline(completed: boolean, error: string, reconfiguring: boolean) {
  if (completed) {return "导入任务已创建";}
  if (error) {return reconfiguring ? "重新配置没有完成" : "上传没有完成";}
  return reconfiguring ? "正在复用文件并重新识别" : "正在上传并验证";
}

function ProgressStages({ busy, completed, error, reconfiguring, uploadPercent }: { busy: boolean; completed: boolean; error: string; reconfiguring: boolean; uploadPercent: number }) {
  const uploadState = uploadPercent >= 76 ? "完成" : busy ? "处理中" : error ? "未完成" : "等待";
  const validationState = uploadPercent >= 92 ? "完成" : uploadPercent >= 76 ? "处理中" : "等待";
  const creationState = completed ? "完成" : uploadPercent >= 92 ? "处理中" : "等待";
  return <div className="import-progress-steps"><div><strong>{reconfiguring ? "复用服务器文件" : "上传文件"}</strong><span>{uploadState}</span></div><div><strong>{reconfiguring ? "应用新配置" : "完整性校验"}</strong><span>{validationState}</span></div><div><strong>创建导入任务</strong><span>{creationState}</span></div></div>;
}

function ProgressActions({ completed, error, onBack, onComplete, reconfiguring }: { completed: boolean; error: string; onBack: () => void; onComplete: () => void; reconfiguring: boolean }) {
  const description = completed
    ? "后台会继续识别、运行检查和游戏信息准备。"
    : reconfiguring ? "文件内容不会再次通过网络上传。" : "上传过程中可以离开页面；已创建的后台任务会继续运行。";
  let action = null;
  if (completed) {action = <button className="button" type="button" onClick={onComplete}>查看任务进度</button>;}
  else if (error) {action = <button className="button secondary" type="button" onClick={onBack}>返回配置</button>;}
  return <div className="import-stage-actions"><span>{description}</span>{action}</div>;
}

function ProgressStep({ busy, completedJobId, error, onBack, onComplete, progress, reconfiguring, uploadPercent }: {
  busy: boolean;
  completedJobId: string;
  error: string;
  onBack: () => void;
  onComplete: () => void;
  progress: string;
  reconfiguring: boolean;
  uploadPercent: number;
}) {
  const completed = Boolean(completedJobId);
  const preparing = reconfiguring ? "正在准备服务器中的既有文件…" : "正在准备安全上传会话…";
  const progressLabel = progress || (reconfiguring ? "准备复用文件…" : "准备上传…");
  return <section className="panel import-progress-card">
    <StatusBadgeLike completed={completed} busy={busy} /><h2>{progressHeadline(completed, error, reconfiguring)}</h2><p>{error || progress || preparing}</p>
    <div className="import-progress-track"><i style={{ width: `${uploadPercent}%` }} /></div><div className="import-progress-line"><span>{completed ? "任务已进入后台处理" : progressLabel}</span><strong>{uploadPercent}%</strong></div>
    <ProgressStages busy={busy} completed={completed} error={error} reconfiguring={reconfiguring} uploadPercent={uploadPercent} />
    <ProgressActions completed={completed} error={error} onBack={onBack} onComplete={onComplete} reconfiguring={reconfiguring} />
  </section>;
}

function reusableSourceFiles(source: ImportDetail | null) {
  return source?.fileOutcomes.filter((file) => file.disposition === "REJECTED" && !file.resolution) ?? [];
}

function uploadProgressPercent(completedJobId: string, progress: string, busy: boolean) {
  if (completedJobId) {return 100;}
  if (/创建导入任务/.test(progress)) {return 92;}
  if (/终结|校验|内容存储/.test(progress)) {return 76;}
  if (/上传/.test(progress)) {return 48;}
  return busy ? 12 : 0;
}

function invalidMultiDiscSelection(contentMode: string, sourceType: string, preflight: MultiDiscPreflight | null, supported: boolean) {
  if (contentMode !== "MULTI_DISC_M3U_V1") {return false;}
  return sourceType !== "DIRECTORY" || !preflight?.detected || !supported || preflight.processableGroupCount === 0;
}

function isProjectContentMode(contentMode: ContentMode) {
  return contentMode === "RPG_MAKER_PROJECT_V1" || contentMode === "ONS_PROJECT_V1" ||
    contentMode === "KIRIKIRI_PROJECT_V1" || contentMode === "BUTTERSCOTCH_PROJECT_V1";
}

function invalidProjectSelection(contentMode: ContentMode, sourceType: string, files: ChosenFile[]) {
  if (!isProjectContentMode(contentMode) || sourceType === "DIRECTORY") {return false;}
  if (files.length !== 1) {return true;}
  const name = files[0].name.toLocaleLowerCase();
  return !name.endsWith(".zip") && !name.endsWith(".7z");
}

function contentModeLabel(contentMode: ContentMode) {
  if (contentMode === "MULTI_DISC_M3U_V1") {return "多盘 M3U";}
  if (contentMode === "RPG_MAKER_PROJECT_V1") {return "RPG Maker 项目";}
  if (contentMode === "ONS_PROJECT_V1") {return "ONS 项目";}
  if (contentMode === "KIRIKIRI_PROJECT_V1") {return "KiriKiri 项目";}
  if (contentMode === "BUTTERSCOTCH_PROJECT_V1") {return "GameMaker 项目";}
  return "普通内容";
}

function effectiveMetadataProvider(contentMode: ContentMode, provider: string) {
  return isProjectContentMode(contentMode) ? "NONE" : provider;
}

function capabilityNotice(sourceType: string, preflight: MultiDiscPreflight | null, target: string, supported: boolean) {
  return sourceType === "DIRECTORY" && preflight?.detected && target && !supported
    ? "当前平台核心不支持多盘游戏，已退回普通游戏内容模式。"
    : "";
}

function multiDiscSubmitLabel(preflight: MultiDiscPreflight | null) {
  const count = preflight?.processableGroupCount ?? 0;
  if (count === 1) {
    if (preflight?.missingDiscCount) {return "继续上传并在审核补齐";}
    return preflight?.rejectedGroupCount ? "继续上传合法分组" : "开始上传";
  }
  if (count > 1) {
    if (preflight?.rejectedGroupCount) {return `继续上传 ${count} 个合法多盘游戏`;}
    return preflight?.missingDiscCount ? `继续上传 ${count} 个多盘游戏` : `开始上传 ${count} 个多盘游戏`;
  }
  return "开始上传";
}

function reconfigurationDefaults(source: ImportDetail | null) {
  return {
    provider: source?.metadataProvider ?? "HASHEOUS",
    target: source?.targetPlatformInstance.id ?? "",
  };
}

function directoryCapabilities(directories: Directory[], target: string) {
  const selected = directories.find((directory) => directory.id === target);
  const contentModes = selected?.importCapabilities?.contentModes ?? [];
  const projectMode = projectContentMode(contentModes);
  return {
    limits: selected?.importCapabilities?.multiDisc ?? MULTI_DISC_DEFAULT_LIMITS,
    projectMode,
    selected,
    supported: contentModes.includes("MULTI_DISC_M3U_V1"),
  };
}

function projectContentMode(contentModes: string[]): ContentMode {
  if (contentModes.includes("RPG_MAKER_PROJECT_V1")) {return "RPG_MAKER_PROJECT_V1";}
  if (contentModes.includes("ONS_PROJECT_V1")) {return "ONS_PROJECT_V1";}
  if (contentModes.includes("KIRIKIRI_PROJECT_V1")) {return "KIRIKIRI_PROJECT_V1";}
  if (contentModes.includes("BUTTERSCOTCH_PROJECT_V1")) {return "BUTTERSCOTCH_PROJECT_V1";}
  return "STANDARD";
}

function sourceMetrics(reconfiguring: boolean, reusableFiles: ReusableFile[], files: ChosenFile[]) {
  if (reconfiguring) {
    return { count: reusableFiles.length, totalBytes: reusableFiles.reduce((total, file) => total + file.sizeBytes, 0) };
  }
  return { count: files.length, totalBytes: files.reduce((total, file) => total + file.size, 0) };
}

export function UploadPicker({ directories, activeTags = [], reconfigureSource = null }: { directories: Directory[]; activeTags?: TagReference[]; reconfigureSource?: ImportDetail | null }) {
  const fileInput = useRef<HTMLInputElement>(null);
  const directoryInput = useRef<HTMLInputElement>(null);
  const [files, setFiles] = useState<ChosenFile[]>([]);
  const [sourceType, setSourceType] = useState<"FILES" | "DIRECTORY">("FILES");
  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [showFiles, setShowFiles] = useState(false);
  const [directoryDialogOpen, setDirectoryDialogOpen] = useState(false);
  const [directoryBrowsing, setDirectoryBrowsing] = useState(false);
  const [directoryBrowseError, setDirectoryBrowseError] = useState("");
  const [pendingDirectory, setPendingDirectory] = useState<PickedDirectory | null>(null);
  const reusableFiles = reusableSourceFiles(reconfigureSource);
  const defaults = reconfigurationDefaults(reconfigureSource);
  const [target, setTarget] = useState(defaults.target);
  const [provider, setProvider] = useState(defaults.provider);
  const [tags, setTags] = useState<TagReference[]>(() => {
    const activeIDs = new Set(activeTags.map((tag) => tag.tagId));
    return (reconfigureSource?.configSnapshot?.tags ?? []).filter((tag) => activeIDs.has(tag.tagId));
  });
  const [contentMode, setContentMode] = useState<ContentMode>(reconfigureSource?.configSnapshot?.contentMode ?? "STANDARD");
  const [preflight, setPreflight] = useState<MultiDiscPreflight | null>(null);
  const [preflighting, setPreflighting] = useState(false);
  const [progress, setProgress] = useState("");
  const [completedJobId, setCompletedJobId] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const router = useRouter();
  const preflightRunRef = useRef(0);
  const multiDiscOptedOutRef = useRef(false);

  function choose(selected: PickedDirectoryFile[], selectedSourceType: "FILES" | "DIRECTORY") {
    const chosen = selected.map(({ file, relativePath }, index) => ({ id: `f${index + 1}`, file, name: file.name, size: file.size, path: relativePath }));
    const selectedDirectory = directories.find((directory) => directory.id === target);
    const selectedContentModes = selectedDirectory?.importCapabilities?.contentModes ?? [];
    const selectedProjectMode = projectContentMode(selectedContentModes);
    setFiles(chosen);
    setSourceType(selectedSourceType);
    setPreflight(null);
    setPreflighting(selectedSourceType === "DIRECTORY" && !isProjectContentMode(selectedProjectMode));
    setContentMode(selectedProjectMode);
    multiDiscOptedOutRef.current = false;
    setShowFiles(false);
    setCompletedJobId("");
    setError("");
    setStep(1);
  }

  const capabilities = directoryCapabilities(directories, target);
  const selectedDirectory = capabilities.selected;
  const multiDiscSupported = capabilities.supported;
  const projectMode = capabilities.projectMode;
  const multiDiscLimits = capabilities.limits;

  useEffect(() => {
    if (reconfigureSource || !files.length || sourceType !== "DIRECTORY" || isProjectContentMode(projectMode)) {return;}
    const run = ++preflightRunRef.current;
    void preflightMultiDisc(files, multiDiscSupported ? multiDiscLimits : MULTI_DISC_DEFAULT_LIMITS).then((result) => {
      if (run !== preflightRunRef.current) {return;}
      setPreflight(result);
      if (result.detected && multiDiscSupported && !multiDiscOptedOutRef.current) {setContentMode("MULTI_DISC_M3U_V1");}
      else if (!result.detected || !multiDiscSupported) {setContentMode("STANDARD");}
    }).catch(() => {
      if (run === preflightRunRef.current) {setPreflight(null);}
    }).finally(() => {
      if (run === preflightRunRef.current) {setPreflighting(false);}
    });
  }, [files, multiDiscLimits, multiDiscSupported, projectMode, reconfigureSource, sourceType, target]);

  function changeTarget(nextTarget: string) {
    const nextDirectory = directories.find((directory) => directory.id === nextTarget);
    const nextContentModes = nextDirectory?.importCapabilities?.contentModes ?? [];
    const nextSupportsMultiDisc = nextContentModes.includes("MULTI_DISC_M3U_V1");
    const nextProjectMode = projectContentMode(nextContentModes);
    setTarget(nextTarget);
    if (files.length && sourceType === "DIRECTORY") {setPreflighting(true);}
    if (isProjectContentMode(nextProjectMode)) {preflightRunRef.current++; setPreflight(null); setContentMode(nextProjectMode); setPreflighting(false);}
    else if (!nextSupportsMultiDisc) {setContentMode("STANDARD");}
    else if (preflight?.detected && !multiDiscOptedOutRef.current) {setContentMode("MULTI_DISC_M3U_V1");}
  }

  async function submitImport() {
    if (busy) {return;}
    setBusy(true); setError(""); setCompletedJobId(""); setStep(3);
    try {
      let imported: Response;
      const selectedProvider = effectiveMetadataProvider(contentMode, provider);
      if (reconfigureSource) {
        setProgress("正在复用已上传文件并按新配置重新识别…");
        imported = await fetch(`/api/v1/admin/imports/${reconfigureSource.importJobId}/reconfigure`, {
          method: "POST",
          credentials: "same-origin",
          headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${reconfigureSource.version}"`, "Idempotency-Key": newUuid() }),
          body: JSON.stringify({ targetPlatformInstanceId: target, metadataProvider: selectedProvider, tagIds: tags.map((tag) => tag.tagId) }),
        });
      } else {
        const purpose = contentMode === "RPG_MAKER_PROJECT_V1" ? "RPG_MAKER_PROJECT"
          : contentMode === "ONS_PROJECT_V1" ? "ONS_PROJECT"
            : contentMode === "KIRIKIRI_PROJECT_V1" ? "KIRIKIRI_PROJECT"
              : contentMode === "BUTTERSCOTCH_PROJECT_V1" ? "BUTTERSCOTCH_PROJECT" : "GENERAL";
        const uploaded = await uploadFiles(files.map((chosen) => ({ file: chosen.file, relativePath: chosen.path })), setProgress, purpose);
        setProgress("正在创建导入任务…");
        imported = await fetch("/api/v1/admin/imports", { method: "POST", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }), body: JSON.stringify({ uploadId: uploaded.uploadId, targetPlatformInstanceId: target, metadataProvider: selectedProvider, contentMode, tagIds: tags.map((tag) => tag.tagId) }) });
      }
      if (!imported.ok) {throw new Error(await responseError(imported, reconfigureSource ? "无法按新配置创建导入任务，请刷新任务后重试" : "上传完成，但无法创建导入任务"));}
      const result = await imported.json() as { importJobId: string };
      setCompletedJobId(result.importJobId);
      setProgress("导入任务已创建，后台会继续识别游戏、检查运行依赖并准备游戏信息。");
      setBusy(false);
      router.push("/admin/imports/tasks");
      router.refresh();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "上传失败"); setBusy(false); setProgress("");
    }
  }

  const multiDiscInvalid = invalidMultiDiscSelection(contentMode, sourceType, preflight, multiDiscSupported);
  const projectInvalid = invalidProjectSelection(contentMode, sourceType, files);
  const metrics = sourceMetrics(Boolean(reconfigureSource), reusableFiles, files);
  const fileCount = metrics.count;
  const totalBytes = metrics.totalBytes;
  const uploadPercent = uploadProgressPercent(completedJobId, progress, busy);
  const visibleCapabilityNotice = capabilityNotice(sourceType, preflight, target, multiDiscSupported);
  const submitLabel = multiDiscSubmitLabel(preflight);

  const resetFiles = () => {
    preflightRunRef.current++;
    setFiles([]); setPreflight(null); setPreflighting(false); setContentMode("STANDARD"); setShowFiles(false);
  };
  const closeDirectoryDialog = () => {
    setDirectoryDialogOpen(false);
    setPendingDirectory(null);
    setDirectoryBrowseError("");
    if (directoryInput.current) {directoryInput.current.value = "";}
  };
  const browseDirectory = async () => {
    setDirectoryBrowseError("");
    if (!directoryPickerAvailable()) {
      directoryInput.current?.click();
      return;
    }
    setDirectoryBrowsing(true);
    try {
      const selected = await pickDirectory();
      if (!selected) {return;}
      if (!selected.files.length) {setDirectoryBrowseError("所选目录中没有可上传文件"); return;}
      setPendingDirectory(selected);
    } catch (caught) {
      setDirectoryBrowseError(caught instanceof Error ? caught.message : "无法读取所选目录");
    } finally {
      setDirectoryBrowsing(false);
    }
  };
  const confirmDirectory = () => {
    if (!pendingDirectory?.files.length) {return;}
    const selected = pendingDirectory.files;
    setDirectoryDialogOpen(false);
    setPendingDirectory(null);
    setDirectoryBrowseError("");
    if (directoryInput.current) {directoryInput.current.value = "";}
    choose(selected, "DIRECTORY");
  };
  const receiveLegacyDirectory = (list: FileList | null) => {
    const selected = droppedDirectory(Array.from(list ?? []));
    if (selected.files.length) {setPendingDirectory(selected);}
  };
  const chooseDroppedFiles = (dropped: FileList) => {
    const selected = Array.from(dropped);
    const directory = selected.some((file) => file.webkitRelativePath.includes("/"));
    choose(droppedDirectory(selected).files, directory ? "DIRECTORY" : "FILES");
  };
  return <div className="import-wizard">
    <ImportStepper reconfiguring={Boolean(reconfigureSource)} step={step} />
    <input ref={fileInput} id="import-files" aria-label="选择导入文件" hidden type="file" multiple onChange={(event) => choose(droppedDirectory(Array.from(event.target.files ?? [])).files, "FILES")} />
    <input ref={directoryInput} id="import-directory" aria-label="选择导入目录" hidden type="file" multiple onChange={(event) => receiveLegacyDirectory(event.target.files)} {...{ webkitdirectory: "" }} />
    {step === 1 ? <SourceStep contentMode={contentMode} files={files} onDrop={chooseDroppedFiles} onNext={() => setStep(2)} onPickDirectory={() => {setPendingDirectory(null); setDirectoryBrowseError(""); setDirectoryDialogOpen(true);}} onPickFiles={() => fileInput.current?.click()} onReset={resetFiles} onToggleFiles={() => setShowFiles((current) => !current)} preflight={preflight} preflighting={preflighting} reconfigureSource={reconfigureSource} reusableFiles={reusableFiles} showFiles={showFiles} totalBytes={totalBytes} /> : null}
    <DirectoryPickerDialog browsing={directoryBrowsing} directory={pendingDirectory} error={directoryBrowseError} open={directoryDialogOpen} onBrowse={() => void browseDirectory()} onCancel={closeDirectoryDialog} onConfirm={confirmDirectory} onDrop={(dropped) => {setDirectoryBrowseError(""); setPendingDirectory(droppedDirectory(Array.from(dropped)));}} />
    {step === 2 ? <ConfigStep activeTags={activeTags} busy={busy} contentMode={contentMode} directories={directories} fileCount={fileCount} multiDiscInvalid={multiDiscInvalid} projectInvalid={projectInvalid} multiDiscLimits={multiDiscLimits} multiDiscSubmitLabel={submitLabel} multiDiscSupported={multiDiscSupported} onBack={() => setStep(1)} onContentMode={(selected) => { setContentMode(selected ? "MULTI_DISC_M3U_V1" : "STANDARD"); multiDiscOptedOutRef.current = !selected; }} onProvider={setProvider} onSubmit={() => void submitImport()} onTags={setTags} onTarget={changeTarget} preflight={preflight} preflighting={preflighting} provider={provider} reconfiguring={Boolean(reconfigureSource)} selectedDirectory={selectedDirectory} sourceIsDirectory={sourceType === "DIRECTORY"} tags={tags} target={target} totalBytes={totalBytes} visibleCapabilityNotice={visibleCapabilityNotice} /> : null}
    {step === 3 ? <ProgressStep busy={busy} completedJobId={completedJobId} error={error} onBack={() => setStep(2)} onComplete={() => { router.push("/admin/imports/tasks"); router.refresh(); }} progress={progress} reconfiguring={Boolean(reconfigureSource)} uploadPercent={uploadPercent} /> : null}
  </div>;
}

function StatusBadgeLike({ completed, busy }: { completed: boolean; busy: boolean }) {
  return <span className={`status ${completed ? "good" : busy ? "info" : "bad"}`}><i />{completed ? "已完成" : busy ? "正在处理" : "需要处理"}</span>;
}
