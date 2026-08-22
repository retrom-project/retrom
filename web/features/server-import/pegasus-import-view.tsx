"use client";

import Link from "next/link";
import { useEffect, useRef, type KeyboardEvent, type ReactNode } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast } from "@/components/flash-toast";
import { TagChips, TagPicker, type TagReference } from "@/components/tag-picker";
import { StatusBadge } from "@/components/ui";
import { formatBytes } from "@/lib/backend";
import type { ServerImportRoot } from "./server-import-manager";
import {
  pegasusOutcomeLabels,
  pegasusPhaseLabels,
  pegasusStateLabels,
  pegasusStateTone,
  type PegasusCollection,
  type PegasusDirectory,
  type PegasusImportSummary,
  type PegasusItem,
  type PegasusPlatformInstance,
} from "./pegasus-import-model";

export type MappingDraft = { action: "" | "IMPORT" | "SKIP"; platformInstanceId: string; tags: TagReference[] };
export type DetailFilters = { query: string; outcome: string; warning: string; collectionId: string };

type RuntimeReason = { code: string; title: string; explanation: string; action: string };

const runtimeReasonCatalog: Record<string, Omit<RuntimeReason, "code">> = {
  LAUNCH_BIOS_MISSING: { title: "缺少运行所需的 BIOS / 基础 ROM", explanation: "当前核心要求的 BIOS 或 Arcade 基础归档没有安装，运行检查无法组成完整启动内容。", action: "前往 BIOS 管理安装缺失文件，再在本任务中重新运行检查。" },
  LAUNCH_PARENT_MISSING: { title: "缺少父 ROM", explanation: "这是 split ROM，当前 ZIP 只包含子机差异，仍需要 DAT 指定的 parent archive。", action: "把缺失的父 ROM ZIP 放入同一 Pegasus 来源并声明到相同目标目录，然后重新运行检查。" },
  ARCADE_CONTENT_MISSING_ENTRY: { title: "ROM ZIP 缺少必要条目", explanation: "ZIP 文件名可以识别，但归档内部没有包含当前活动 DAT 要求的全部 ROM 条目。", action: "换用与当前核心和 DAT 版本匹配的完整 ROM ZIP。" },
  ARCADE_DEPENDENCY_MISMATCH: { title: "父 ROM 或 BIOS 内容不匹配", explanation: "依赖归档存在，但其中的文件名、大小或校验信息与当前活动 DAT 不一致。", action: "替换为与当前核心和 DAT 版本匹配的依赖归档。" },
  UNSUPPORTED_MERGED_ROMSET: { title: "当前不支持 merged ROM set", explanation: "该归档需要从 merged ROM set 中拆分依赖，当前自动准备流程无法安全构造可审核的运行内容。", action: "改用 split 或 non-merged ROM set 后重新导入。" },
  UNSUPPORTED_CHD: { title: "当前核心不支持此 CHD 组合", explanation: "DAT 识别到了 CHD 依赖，但当前核心的导入能力无法装配这种内容。", action: "换用该核心支持的 ROM set，或映射到支持此内容的游戏目录。" },
  ARCADE_DAT_UNAVAILABLE: { title: "Arcade DAT 不可用", explanation: "目标核心固定的内置 DAT 尚未准备完成，无法判断 ROM 和依赖闭包。", action: "检查服务依赖准备和 Ready 状态，恢复后重新运行检查。" },
  ARCADE_DEPENDENCY_CYCLE: { title: "DAT 依赖关系存在循环", explanation: "内置 DAT 中这台 machine 的 parent / BIOS 依赖形成循环，无法构造有限运行闭包。", action: "记录核心与 machine 信息并升级到修复该目录数据的 Retrom 版本。" },
  MULTI_DISC_FILE_MISSING: { title: "多盘游戏缺少引用文件", explanation: "M3U 引用的光盘文件没有全部出现在冻结的来源快照中。", action: "补齐列出的光盘文件，并保持 M3U 中的相对路径一致。" },
  LAUNCH_CORE_VALIDATION_UNAVAILABLE: { title: "核心运行检查不可用", explanation: "核心依赖或验证器当前无法完成检查。", action: "确认核心 artifact、DAT 和 BIOS 状态后重新运行检查。" },
  PEGASUS_LIBRARY_IMPORT_FAILED: { title: "内部导入检查未完成", explanation: "内容已经复制，但复用游戏入库检查时发生了可重试错误。", action: "点击页面顶部的“重新运行检查”重试，不需要重新扫描目录。" },
};

const failureReasonCatalog: Record<string, Omit<RuntimeReason, "code">> = {
  SOURCE_FILE_LIMIT_EXCEEDED: { title: "Arcade companion 候选数量超过内部上限", explanation: "系统为单个游戏组装了过多来源 ZIP，内部入库在内容检查前就拒绝了请求。", action: "这是服务端 companion 选择范围问题；升级修复后直接重新运行检查，不需要调整 ROM 目录。" },
  LIBRARY_IMPORT_INPUT_INVALID: { title: "内部入库输入不符合约束", explanation: "Pegasus 已复制来源，但交给内部游戏入库管线的参数或文件集合未通过预检。", action: "结合内部操作、相对路径和技术详情排查组装参数，然后重新运行检查。" },
  MULTI_DISC_MODE_UNAVAILABLE: { title: "多盘入库能力未启用", explanation: "当前服务配置不允许处理该 M3U 多盘集合。", action: "启用多盘导入能力并确认目标核心支持该内容后重新运行检查。" },
  DATABASE_BUSY: { title: "数据库写入被占用", explanation: "内部入库写事务在允许时间内未能取得 SQLite 写锁。", action: "检查是否存在长事务或并发维护任务，待写锁释放后重新运行检查。" },
  DATABASE_CONSTRAINT_FAILED: { title: "内部数据约束冲突", explanation: "写入内部入库记录时触发了数据库约束。", action: "使用关联操作、任务 ID 和技术详情定位冲突记录，再重新运行检查。" },
  OPERATION_TIMEOUT: { title: "内部操作超时", explanation: "该条目在规定时间内没有完成内部入库步骤。", action: "检查磁盘与数据库响应时间，然后重新运行检查。" },
  OPERATION_CANCELLED: { title: "内部操作被取消", explanation: "该条目的内部处理上下文在完成前被取消。", action: "确认任务未被管理员取消且服务进程稳定，然后重新运行检查。" },
  METADATA_JSON_INVALID: { title: "冻结的元数据无法解码", explanation: "扫描阶段保存的规范化元数据不是有效 JSON，发布步骤无法读取。", action: "使用 Pegasus Item ID 和内部 ImportItem ID 定位记录；修复服务端元数据序列化后重新运行检查。" },
  INTERNAL_OPERATION_FAILED: { title: "内部操作发生未分类错误", explanation: "服务端保留了失败阶段、操作名和经过约束的技术详情。", action: "按下面的排查上下文定位对应内部操作；如问题可恢复，可重新运行检查。" },
};

function runtimeReason(item: PegasusItem): RuntimeReason | null {
  if (item.executionState === "REVIEW_PENDING" && item.runtimeCheck?.status === "READY" && !item.errorCode) {return null;}
  const code = item.runtimeCheck?.code ?? item.errorCode ?? item.discoveryCode;
  if (!code) {return null;}
  const failureReason = item.failureDetails ? failureReasonCatalog[item.failureDetails.causeCode] : null;
  if (failureReason) {return { code, ...failureReason };}
  return { code, ...(runtimeReasonCatalog[code] ?? { title: "处理被阻断", explanation: "服务端返回了稳定诊断码，请结合下面的检查证据处理。", action: "按缺失文件和依赖信息修正来源后重新导入或重试。" }) };
}

function FailureDetails({ item }: { item: PegasusItem }) {
  const failure = item.failureDetails;
  if (!failure) {return null;}
  return <section className="pegasus-internal-diagnostic" aria-label="内部排查信息"><h4>内部排查信息</h4><dl>
    <div><dt>失败阶段</dt><dd><code>{failure.stage}</code></dd></div><div><dt>内部操作</dt><dd><code>{failure.operation}</code></dd></div>
    <div><dt>底层原因分类</dt><dd><code>{failure.causeCode}</code></dd></div><div><dt>Pegasus Item ID</dt><dd><code>{item.id}</code></dd></div>
    {failure.relativePath ? <div><dt>来源相对路径</dt><dd><code>{failure.relativePath}</code></dd></div> : null}
    {failure.observedFileCount !== null ? <div><dt>组装文件数量</dt><dd><code>{failure.observedFileCount}{failure.allowedFileCount !== null ? ` / 上限 ${failure.allowedFileCount}` : ""}</code></dd></div> : null}
    {failure.libraryImportJobId ? <div><dt>内部 ImportJob</dt><dd><code>{failure.libraryImportJobId}</code></dd></div> : null}
    {failure.libraryImportItemId ? <div><dt>内部 ImportItem</dt><dd><code>{failure.libraryImportItemId}</code></dd></div> : null}
  </dl><div className="pegasus-technical-detail"><strong>技术详情</strong><code>{failure.technicalDetail || "服务端没有返回额外文本；请使用阶段、操作和原因码定位。"}</code></div></section>;
}

function RuntimeEvidence({ item }: { item: PegasusItem }) {
  const check = item.runtimeCheck;
  if (!check) {return null;}
  return <><dl>{check.coreId ? <div><dt>检查核心</dt><dd>{check.coreName || check.coreId} <code>{check.coreId}</code></dd></div> : null}{check.machine ? <div><dt>识别 machine</dt><dd><code>{check.machine}</code></dd></div> : null}{check.missingEntries.length ? <div><dt>缺失文件 / 条目</dt><dd>{check.missingEntries.map((entry) => <code key={entry}>{entry}</code>)}</dd></div> : null}{check.mismatchedEntries.length ? <div><dt>不匹配条目</dt><dd>{check.mismatchedEntries.map((entry) => <code key={entry}>{entry}</code>)}</dd></div> : null}{check.missingDiscs.length ? <div><dt>缺失光盘</dt><dd>{check.missingDiscs.map((disc) => <code key={`${disc.ordinal}-${disc.sourceReference}`}>Disc {disc.ordinal}: {disc.sourceReference}</code>)}</dd></div> : null}</dl>
    {check.dependencies.length ? <div className="pegasus-runtime-dependencies"><h4>依赖明细</h4>{check.dependencies.map((dependency) => <article key={`${dependency.kind}-${dependency.machine}`}><p><strong>{dependency.kind === "PARENT" ? "父 ROM" : "BIOS / 基础 ROM"}</strong><code>{dependency.expectedLogicalName || `${dependency.machine}.zip`}</code><span>{dependency.state}</span></p>{dependency.requiredBy ? <small>由 {dependency.requiredBy} 依赖</small> : null}{dependency.requiredEntries.length ? <details><summary>查看 {dependency.requiredEntries.length} 个必需条目</summary><code>{dependency.requiredEntries.join(" · ")}</code></details> : null}</article>)}</div> : null}
    {check.bios.length ? <div className="pegasus-runtime-dependencies"><h4>BIOS 明细</h4>{check.bios.map((bios) => <article key={bios.logicalName}><p><strong>{bios.requirementMode}</strong><code>{bios.logicalName}</code><span>{bios.installationStatus ?? "未安装"}</span></p></article>)}</div> : null}</>;
}

function RuntimeCheckDetails({ item }: { item: PegasusItem }) {
  const reason = runtimeReason(item);
  if (!reason || item.executionState === "PUBLISHED" || item.executionState === "SKIPPED_EXISTING") {return null;}
  return <details className="pegasus-runtime-diagnostic"><summary>查看具体原因与处理建议</summary><div className="pegasus-runtime-diagnostic-body"><header><div><strong>{reason.title}</strong><p>{reason.explanation}</p></div><code>{reason.code}</code></header><FailureDetails item={item} /><RuntimeEvidence item={item} /><p className="pegasus-runtime-action"><strong>处理建议</strong>{reason.action}{reason.code === "LAUNCH_BIOS_MISSING" ? <Link href="/admin/bios">打开 BIOS 管理</Link> : null}</p></div></details>;
}

function trapFocus(drawer: HTMLElement | null, event: KeyboardEvent<HTMLElement>, busy: boolean, onClose: () => void) {
  if (event.key === "Escape" && !busy) {event.preventDefault(); onClose();}
  if (event.key !== "Tab") {return;}
  const focusable = Array.from(drawer?.querySelectorAll<HTMLElement>("button:not(:disabled),input:not(:disabled),select:not(:disabled),a[href],[tabindex]:not([tabindex='-1'])") ?? []);
  if (!focusable.length) {return;}
  const first = focusable[0]; const last = focusable[focusable.length - 1];
  if (event.shiftKey && document.activeElement === first) {event.preventDefault(); last.focus();}
  else if (!event.shiftKey && document.activeElement === last) {event.preventDefault(); first.focus();}
}

function SelectionStep({ roots, rootId, path, breadcrumbs, directories, cursor, loading, busy, selectedRoot, onRoot, onPath, onMore }: { roots: ServerImportRoot[]; rootId: string; path: string; breadcrumbs: string[]; directories: PegasusDirectory[]; cursor: string | null; loading: boolean; busy: boolean; selectedRoot?: ServerImportRoot; onRoot: (id: string) => void; onPath: (path: string) => void; onMore: () => void }) {
  return <><fieldset className="server-root-options"><legend>服务器位置</legend>{roots.map((root) => <label key={root.id}><input type="radio" name="pegasus-root" checked={rootId === root.id} disabled={busy || root.status !== "AVAILABLE"} onChange={() => onRoot(root.id)} /><span><strong>{root.label}</strong><small>{root.status === "AVAILABLE" ? "可用" : "不可用"}</small></span></label>)}</fieldset><div className="server-directory-browser"><nav aria-label="当前目录"><button type="button" onClick={() => onPath("")} disabled={!path || busy}>根目录</button>{breadcrumbs.map((part, index) => <button type="button" key={`${part}-${index}`} disabled={index === breadcrumbs.length - 1 || busy} onClick={() => onPath(breadcrumbs.slice(0, index + 1).join("/"))}>/ {part}</button>)}</nav>{directories.length ? <><ul>{directories.map((directory) => <li key={directory.relativePath}><button type="button" disabled={busy} onClick={() => onPath(directory.relativePath)}><AppIcon name="folder" /><span>{directory.name}</span></button></li>)}</ul>{cursor ? <button type="button" className="button secondary compact" disabled={loading || busy} onClick={onMore}>{loading ? "正在读取…" : "加载更多目录"}</button> : null}</> : <p role="status">{loading ? "正在读取子目录…" : "当前目录没有可进入的子目录。"}</p>}</div><div className="server-import-selection-summary"><strong>{selectedRoot?.label ?? "未选择"} / {path || "根目录"}</strong><span>先异步读取 metadata、文件大小与稳定 facts；确认映射后才读取完整 ROM bytes。</span></div></>;
}

function CollectionMapping({ collection, draft, instances, tags, busy, onChange }: { collection: PegasusCollection; draft: MappingDraft; instances: PegasusPlatformInstance[]; tags: TagReference[]; busy: boolean; onChange: (draft: MappingDraft) => void }) {
  const value = draft.action === "SKIP" ? "SKIP" : draft.platformInstanceId ? `IMPORT:${draft.platformInstanceId}` : "";
  function select(next: string) {
    if (next === "SKIP") {onChange({ action: "SKIP", platformInstanceId: "", tags: [] }); return;}
    if (next.startsWith("IMPORT:")) {onChange({ action: "IMPORT", platformInstanceId: next.slice(7), tags: draft.tags }); return;}
    onChange({ action: "", platformInstanceId: "", tags: [] });
  }
  return <article><div><h3>{collection.name}</h3><p>{collection.metadataRelativePath} · segment {collection.segmentOrdinal + 1}</p><small>{collection.shortName ? `shortname: ${collection.shortName} · ` : ""}{collection.gameCount} 个游戏 · {collection.issueCount} 个阻断/问题</small></div><label><span>处理方式</span><select aria-label={`${collection.name} 处理方式`} value={value} onChange={(event) => select(event.target.value)}><option value="">请选择，不会自动映射</option><option value="SKIP">跳过此集合</option>{instances.map((instance) => <option value={`IMPORT:${instance.id}`} key={instance.id}>导入到 {instance.name} · {instance.defaultCoreName}</option>)}</select></label>{draft.action === "IMPORT" ? <div className="pegasus-collection-tags"><TagPicker label={`${collection.name} 的默认标签`} options={tags} selected={draft.tags} disabled={busy} onChange={(selected) => onChange({ ...draft, tags: selected })} description="此集合生成的每个待审核游戏都会继承这些标签。" /></div> : null}</article>;
}

function MappingStep({ plan, collections, mappings, instances, activeTags, batchTags, batchStatus, busy, onBatchTags, onApplyBatch, onMapping }: { plan: PegasusImportSummary | null; collections: PegasusCollection[]; mappings: Record<string, MappingDraft>; instances: PegasusPlatformInstance[]; activeTags: TagReference[]; batchTags: TagReference[]; batchStatus: string; busy: boolean; onBatchTags: (tags: TagReference[]) => void; onApplyBatch: () => void; onMapping: (id: string, draft: MappingDraft) => void }) {
  if (plan?.state === "SCANNING") {return <div className="pegasus-scan-progress" aria-live="polite"><span className="button-spinner" /><h3>{plan.phase ? pegasusPhaseLabels[plan.phase] : "扫描准备中"}</h3><p>任务离开页面后仍会继续。当前发现 {plan.counts.metadata} 个 metadata、{plan.counts.collections} 个集合、{plan.counts.games} 个游戏。</p></div>;}
  if (plan?.state === "FAILED") {return <div className="runtime-inline-empty"><h3>扫描未完成</h3><p>{plan.lastErrorCode ?? "扫描任务失败"}</p></div>;}
  if (plan?.state !== "AWAITING_MAPPING") {return null;}
  if (instances.length === 0) {return <div className="runtime-inline-empty"><h3>还没有游戏目录</h3><p>请先进入游戏目录，使用“一键创建推荐目录”建立映射目标，再回来继续这次 Pegasus 导入。</p><Link className="button" href="/admin/platform-instances">前往游戏目录</Link></div>;}
  return <><div className="pegasus-scan-summary"><div><span>Metadata</span><strong>{plan.counts.metadata}</strong></div><div><span>Collection</span><strong>{plan.counts.collections}</strong></div><div><span>Game</span><strong>{plan.counts.games}</strong></div><div><span>发现视频</span><strong>{plan.counts.videos}</strong></div></div><p className="pegasus-mapping-note">每个 source collection 必须明确选择游戏目录或跳过；Retrom 不会根据名称、扩展名或 launch 命令猜测。</p><section className="pegasus-batch-tags" aria-labelledby="pegasus-batch-tags-title"><header><div><h3 id="pegasus-batch-tags-title">批量添加默认标签</h3><p>选择一次后追加到所有未跳过 Collection，不覆盖已有选择；下方仍可逐项增删。</p></div><span>{collections.reduce((total, collection) => total + collection.gameCount, 0)} 个游戏</span></header><TagPicker label="批次标签" options={activeTags} selected={batchTags} disabled={busy} onChange={onBatchTags} description="标签必须先在标签管理中建立。点击应用后，尚未选择处理方式的 Collection 也会保留这些默认标签。" /><div className="pegasus-batch-tag-actions"><button type="button" className="button secondary compact" disabled={busy || !batchTags.length} onClick={onApplyBatch}>应用到所有未跳过 Collection</button>{batchStatus ? <p role="status">{batchStatus}</p> : null}</div></section><div className="pegasus-collection-list">{collections.map((collection) => <CollectionMapping key={collection.id} collection={collection} draft={mappings[collection.id] ?? { action: "", platformInstanceId: "", tags: [] }} instances={instances} tags={activeTags} busy={busy} onChange={(draft) => onMapping(collection.id, draft)} />)}</div></>;
}

function ReviewStep({ plan, mapped, skipped, taggedCollections, taggedGames, mappedTags }: { plan: PegasusImportSummary; mapped: number; skipped: number; taggedCollections: number; taggedGames: number; mappedTags: TagReference[] }) {
  return <><div className="pegasus-review-table"><div><span>来源</span><strong>{plan.root.label} / {plan.sourceRelativePath || "根目录"}</strong></div><div><span>映射</span><strong>{mapped} 个处理 · {skipped} 个跳过</strong></div><div><span>默认标签覆盖</span><strong>{taggedCollections} 个 Collection · {taggedGames} 个游戏</strong></div><div><span>可处理 / 源内容阻断</span><strong>{plan.counts.processable} / {plan.counts.blocked} 个游戏</strong></div><div><span>封面 / 视频</span><strong>{plan.counts.covers} / {plan.counts.videos}</strong></div><div><span>预计最多读取</span><strong>{formatBytes(plan.counts.estimatedSourceBytes)}</strong></div><div><span>发布方式</span><strong>全部进入待审核，由管理员逐项决定</strong></div></div>{mappedTags.length ? <div className="pegasus-review-tags"><span>本批使用的标签</span><TagChips tags={mappedTags} /></div> : null}<p className="pegasus-mapping-note">开始时会重新核对 metadata digest 与源文件 facts。后台只准备来源与运行检查，不会创建游戏；已经生成的审核事项在取消任务后仍会保留。</p></>;
}

type DrawerViewProps = { roots: ServerImportRoot[]; rootId: string; path: string; breadcrumbs: string[]; directories: PegasusDirectory[]; directoryCursor: string | null; directoryLoading: boolean; selectedRoot?: ServerImportRoot; step: 1 | 2 | 3; plan: PegasusImportSummary | null; collections: PegasusCollection[]; mappings: Record<string, MappingDraft>; availableInstances: PegasusPlatformInstance[]; activeTags: TagReference[]; batchTags: TagReference[]; batchStatus: string; busy: boolean; error: string; mapped: number; skipped: number; taggedCollections: number; taggedGames: number; mappedTags: TagReference[]; mappingComplete: boolean; onRoot: (id: string) => void; onPath: (path: string) => void; onMore: () => void; onBatchTags: (tags: TagReference[]) => void; onApplyBatch: () => void; onMapping: (id: string, draft: MappingDraft) => void; onClose: () => void; onScan: () => void; onConfirm: () => void; onStart: () => void; onDismissError: () => void };

function stepClass(step: number, expected: number) {
  if (step === expected) {return "is-active";}
  if (step > expected) {return "is-complete";}
  return "";
}

function DrawerSteps({ step }: { step: 1 | 2 | 3 }) {
  return <ol className="pegasus-stepper" aria-label="导入步骤"><li className={stepClass(step, 1)}><span>1</span>选择目录</li><li className={stepClass(step, 2)}><span>2</span>检查与映射</li><li className={stepClass(step, 3)}><span>3</span>确认审核计划</li></ol>;
}

function DrawerBody({ props }: { props: DrawerViewProps }) {
  if (props.step === 1) {return <SelectionStep roots={props.roots} rootId={props.rootId} path={props.path} breadcrumbs={props.breadcrumbs} directories={props.directories} cursor={props.directoryCursor} loading={props.directoryLoading} busy={props.busy} selectedRoot={props.selectedRoot} onRoot={props.onRoot} onPath={props.onPath} onMore={props.onMore} />;}
  if (props.step === 2) {return <MappingStep plan={props.plan} collections={props.collections} mappings={props.mappings} instances={props.availableInstances} activeTags={props.activeTags} batchTags={props.batchTags} batchStatus={props.batchStatus} busy={props.busy} onBatchTags={props.onBatchTags} onApplyBatch={props.onApplyBatch} onMapping={props.onMapping} />;}
  if (props.plan) {return <ReviewStep plan={props.plan} mapped={props.mapped} skipped={props.skipped} taggedCollections={props.taggedCollections} taggedGames={props.taggedGames} mappedTags={props.mappedTags} />;}
  return null;
}

function DrawerFooter({ props }: { props: DrawerViewProps }) {
  return <footer><button type="button" className="button secondary" disabled={props.busy} onClick={props.onClose}>关闭</button>{props.step === 1 ? <button type="button" className="button" disabled={props.busy || !props.rootId || props.selectedRoot?.status !== "AVAILABLE"} onClick={props.onScan}>{props.busy ? "正在创建…" : "扫描此目录"}</button> : null}{props.step === 2 && props.plan?.state === "AWAITING_MAPPING" ? <button type="button" className="button" disabled={props.busy || !props.mappingComplete} onClick={props.onConfirm}>{props.busy ? "正在保存…" : "确认映射"}</button> : null}{props.step === 3 ? <button type="button" className="button" disabled={props.busy} onClick={props.onStart}>{props.busy ? "正在启动…" : "开始准备审核事项"}</button> : null}</footer>;
}

export function PegasusImportDrawerView(props: DrawerViewProps) {
  const drawer = useRef<HTMLElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const root = document.documentElement; const body = document.body;
    const rootOverflow = root.style.overflow; const bodyOverflow = body.style.overflow; const bodyPadding = body.style.paddingRight;
    const scrollbar = root.clientWidth > 0 ? Math.max(0, window.innerWidth - root.clientWidth) : 0;
    const padding = Number.parseFloat(window.getComputedStyle(body).paddingRight) || 0;
    root.style.overflow = "hidden"; body.style.overflow = "hidden";
    if (scrollbar > 0) {body.style.paddingRight = `${padding + scrollbar}px`;}
    closeButton.current?.focus({ preventScroll: true });
    return () => {root.style.overflow = rootOverflow; body.style.overflow = bodyOverflow; body.style.paddingRight = bodyPadding; if (previous?.isConnected) {previous.focus({ preventScroll: true });}};
  }, []);
  return <><button type="button" className="runtime-drawer-backdrop" aria-label="关闭 Pegasus 导入" disabled={props.busy} onClick={props.onClose} /><aside ref={drawer} className="runtime-drawer server-import-drawer pegasus-import-drawer" role="dialog" aria-modal="true" aria-labelledby="pegasus-import-title" onKeyDown={(event) => trapFocus(drawer.current, event, props.busy, props.onClose)}><header><div><StatusBadge tone="info">Pegasus ROM</StatusBadge><h2 id="pegasus-import-title">从 Pegasus 目录准备审核事项</h2><p>只显示允许 root 内的相对目录；扫描不会复制 ROM 或创建游戏。</p></div><button ref={closeButton} type="button" className="runtime-drawer-close" aria-label="关闭" disabled={props.busy} onClick={props.onClose}><AppIcon name="x" /></button></header><DrawerSteps step={props.step} /><div className="runtime-drawer-body"><DrawerBody props={props} /></div><DrawerFooter props={props} /></aside><Toast toast={props.error ? { message: props.error, tone: "bad" } : null} onDismiss={props.onDismissError} /></>;
}

function outcomeTone(item: PegasusItem): "good" | "warn" | "bad" | "info" {
  if (item.executionState === "PUBLISHED") {return "good";}
  if (item.executionState === "REVIEW_PENDING") {return "info";}
  if (item.executionState.startsWith("BLOCKED") || ["SOURCE_CHANGED", "READ_FAILED", "COMMIT_FAILED"].includes(item.executionState)) {return "bad";}
  return "warn";
}

function ItemAction({ item, reviewURL }: { item: PegasusItem; reviewURL: string }) {
  const reviewHref = item.reviewItemId ? `/admin/reviews/${item.reviewItemId}?returnTo=${encodeURIComponent(reviewURL)}` : null;
  if (reviewHref && item.executionState === "REVIEW_PENDING") {return <Link className="button compact" href={reviewHref}>{item.runtimeCheck?.status === "READY" ? "审核并决定" : "处理运行问题"}</Link>;}
  if (item.publishedGameId) {return <Link href={`/games/${item.publishedGameId}`}>查看游戏</Link>;}
  if (item.existingGameId) {return <Link href={`/games/${item.existingGameId}`}>已有游戏</Link>;}
  if (item.executionState === "REVIEW_DISCARDED") {return <small>管理员已在审核队列中丢弃</small>;}
  if (item.discoveryCode === "PEGASUS_MULTIPLE_LAUNCH_FILES_UNSUPPORTED") {return <small>Pegasus 把多个文件视为可选启动项；请整理为单文件或受支持的 Saturn M3U。</small>;}
  return <span>—</span>;
}

function ResultRow({ item, reviewURL }: { item: PegasusItem; reviewURL: string }) {
  const reason = runtimeReason(item);
  const result = reason?.title ?? item.errorCode ?? item.discoveryCode ?? (item.warnings.map((warning) => warning.code).join("、") || (item.executionState === "REVIEW_PENDING" ? "等待管理员作出审核决定" : "无附加结果码"));
  const mediaTone = (state: string) => state === "READY" ? "good" as const : state === "WARNING" ? "warn" as const : "info" as const;
  return <article role="row"><div role="cell"><h3>{item.title}</h3><TagChips tags={item.tags} limit={2} ariaLabel={`${item.title} 的标签`} /><p>{item.collectionName ?? "无有效 Collection"} → {item.targetPlatformInstanceName ?? "未映射"}</p><small>{item.metadataRelativePath} · {item.contentKind ?? "内容类型待定"}</small></div><div role="cell" className="pegasus-result-media"><StatusBadge tone={mediaTone(item.media.cover)}>封面 {item.media.cover}</StatusBadge><StatusBadge tone={mediaTone(item.media.video)}>视频 {item.media.video}</StatusBadge></div><div role="cell"><StatusBadge tone={outcomeTone(item)}>{pegasusOutcomeLabels[item.executionState]}</StatusBadge><small>{result}</small></div><div role="cell"><ItemAction item={item} reviewURL={reviewURL} /></div><div role="cell" className="pegasus-runtime-diagnostic-cell"><RuntimeCheckDetails item={item} /></div></article>;
}

type DetailViewProps = { summary: PegasusImportSummary; items: PegasusItem[]; nextCursor: string | null; draft: DetailFilters; collections: PegasusCollection[]; busy: boolean; error: string; cancelOpen: boolean; mappingOpen: boolean; mappingDrawer: ReactNode; onDraft: (draft: DetailFilters) => void; onApplyFilters: () => void; onCancelOpen: (open: boolean) => void; onCancel: () => void; onRetry: () => void; onMappingOpen: (open: boolean) => void; onLoadMore: () => void; onDismissError: () => void };

function DetailHeader({ props, reviewURL, phase }: { props: DetailViewProps; reviewURL: string; phase: string }) {
  return <section className="server-import-detail-head panel"><div><StatusBadge tone={pegasusStateTone(props.summary.state)}>{pegasusStateLabels[props.summary.state]}</StatusBadge><h2>{props.summary.root.label} / {props.summary.sourceRelativePath || "根目录"}</h2><p aria-live="polite">{phase}</p></div><div>{["SCANNING", "QUEUED", "RUNNING"].includes(props.summary.state) ? <button type="button" className="button secondary" disabled={props.busy} onClick={() => props.onCancelOpen(true)}>取消任务</button> : null}{props.summary.retryable ? <button type="button" className="button secondary" disabled={props.busy} onClick={props.onRetry}>重试失败条目</button> : null}{props.summary.counts.reviewPending ? <Link href={reviewURL} className="button">逐项审核 {props.summary.counts.reviewPending} 个游戏</Link> : null}{props.summary.state === "AWAITING_MAPPING" ? <button type="button" className="button" disabled={props.busy} onClick={() => props.onMappingOpen(true)}>继续映射</button> : <Link href="/admin/imports/server?action=pegasus" className="button secondary">新建 Pegasus 导入</Link>}</div></section>;
}

function DetailSummary({ summary }: { summary: PegasusImportSummary }) {
  return <section className="runtime-kpis" aria-label="Pegasus 导入摘要"><article><small>扫描范围</small><strong>{summary.counts.games}</strong><p>{summary.counts.collections} 个 Collection · {summary.counts.processable} 项可处理</p></article><article className={summary.counts.reviewPending ? "has-warning" : ""}><small>等待逐项审核</small><strong>{summary.counts.reviewPending}</strong><p>不会自动发布到游戏库</p></article><article className="has-success"><small>已发布 / 已丢弃 / 已存在</small><strong>{summary.counts.published} / {summary.counts.reviewDiscarded} / {summary.counts.existing}</strong><p>均保留来源与审核证据</p></article><article className={summary.counts.blocked + summary.counts.failed ? "has-danger" : ""}><small>源内容阻断 / 任务失败</small><strong>{summary.counts.blocked} / {summary.counts.failed}</strong><p>{summary.counts.mediaWarnings} 个媒体警告</p></article></section>;
}

function DetailResults({ props, reviewURL }: { props: DetailViewProps; reviewURL: string }) {
  return <section className="server-import-results"><div className="runtime-section-heading"><div><h2>准备与审核结果</h2><p>后台只准备审核事项；待审核条目必须由管理员逐项决定是否发布。</p></div><span>{props.items.length} / {props.summary.counts.games} 项</span></div><form className="server-import-result-filters panel pegasus-result-filters" onSubmit={(event) => {event.preventDefault(); props.onApplyFilters();}}><label><span>搜索标题</span><input type="search" value={props.draft.query} onChange={(event) => props.onDraft({ ...props.draft, query: event.target.value })} /></label><label><span>结果</span><select value={props.draft.outcome} onChange={(event) => props.onDraft({ ...props.draft, outcome: event.target.value })}><option value="">全部结果</option>{Object.entries(pegasusOutcomeLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label><label><span>媒体警告</span><input value={props.draft.warning} placeholder="例如 PEGASUS_VIDEO_UNSUPPORTED" onChange={(event) => props.onDraft({ ...props.draft, warning: event.target.value })} /></label><label><span>Collection</span><select value={props.draft.collectionId} onChange={(event) => props.onDraft({ ...props.draft, collectionId: event.target.value })}><option value="">全部 Collection</option>{props.collections.map((collection) => <option value={collection.id} key={collection.id}>{collection.name}</option>)}</select></label><button type="submit" className="button secondary compact" disabled={props.busy}>{props.busy ? "正在筛选…" : "应用筛选"}</button></form><div className="pegasus-result-table" role="table" aria-label="Pegasus 导入结果">{props.items.map((item) => <ResultRow key={item.id} item={item} reviewURL={reviewURL} />)}</div>{props.nextCursor ? <button type="button" className="button secondary server-import-history-more" disabled={props.busy} onClick={props.onLoadMore}>加载更多结果</button> : null}</section>;
}

export function PegasusImportDetailView(props: DetailViewProps) {
  const reviewURL = `/admin/reviews?pegasusImportId=${encodeURIComponent(props.summary.id)}`;
  const terminal = ["COMPLETED", "PARTIAL_FAILURE", "FAILED", "CANCELLED", "EXPIRED"].includes(props.summary.state);
  const phase = props.summary.phase ? pegasusPhaseLabels[props.summary.phase] : terminal ? props.summary.counts.reviewPending ? `已准备 ${props.summary.counts.reviewPending} 个待审核游戏` : "后台准备任务已结束" : "等待处理";
  return <div className="server-import-detail-page pegasus-detail-page"><DetailHeader props={props} reviewURL={reviewURL} phase={phase} />
    {props.summary.counts.reviewPending ? <section className="pegasus-review-callout panel"><div><span>下一步 · 人工审核</span><h2>内容已准备好，但尚未进入游戏库</h2><p>请逐条核对运行检查、标题、封面和视频。只有点击“通过并发布”的游戏才会出现在游戏库；系统不提供批量通过。</p></div><Link href={reviewURL} className="button">打开这批审核队列</Link></section> : null}
    <DetailSummary summary={props.summary} />
    {props.summary.lastErrorCode ? <p className="server-import-error panel"><strong>{props.summary.lastErrorCode}</strong><span>外部 source 不属于备份；目录变化时请按结果提示重扫或重试。</span></p> : null}
    <DetailResults props={props} reviewURL={reviewURL} />
    <ConfirmDialog open={props.cancelOpen} title="取消这次 Pegasus 准备任务？" description="已经生成的审核事项会保留，尚未处理的项目会在安全检查点停止。" confirmLabel="确认取消" tone="danger" busy={props.busy} onCancel={() => props.onCancelOpen(false)} onConfirm={props.onCancel} />
    {props.mappingOpen ? props.mappingDrawer : null}<Toast toast={props.error ? { message: props.error, tone: "bad" } : null} onDismiss={props.onDismissError} /></div>;
}
