"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Toast } from "@/components/flash-toast";
import { StatusBadge } from "@/components/ui";
import { api, writeHeaders } from "@/lib/api/client";
import type { components } from "@/lib/api/generated/schema";
import { formatBytes } from "@/lib/backend";
import { newUuid } from "@/lib/crypto";
import { responseError } from "@/lib/upload";
import type { ServerImportRoot } from "./server-import-manager";
import { TagChips, TagPicker, type TagReference } from "@/components/tag-picker";

export type PegasusImportSummary = components["schemas"]["PegasusImportSummary"];
export type PegasusImportList = components["schemas"]["PegasusImportList"];
export type PegasusCollection = components["schemas"]["PegasusSourceCollection"];
export type PegasusItemList = components["schemas"]["PegasusItemList"];
export type PegasusItem = components["schemas"]["PegasusItem"];
type Directory = components["schemas"]["ServerImportDirectory"];

export type PegasusPlatformInstance = {
  id: string;
  name: string;
  platformName: string;
  defaultCoreId: string;
  defaultCoreName: string;
  enabled: boolean;
};

export const pegasusStateLabels: Record<PegasusImportSummary["state"], string> = {
  SCANNING: "正在扫描", AWAITING_MAPPING: "等待映射", QUEUED: "等待导入", RUNNING: "正在导入",
  PARTIAL_FAILURE: "部分需要处理", COMPLETED: "审核事项已生成", CANCEL_REQUESTED: "正在取消", CANCELLED: "已取消",
  FAILED: "任务失败", EXPIRED: "计划已过期",
};

const phaseLabels: Record<NonNullable<PegasusImportSummary["phase"]>, string> = {
  DISCOVERING_METADATA: "发现 metadata", PARSING_METADATA: "解析 metadata", RESOLVING_SOURCES: "核对源文件",
  COPYING_CONTENT: "复制内容", VALIDATING: "运行检查", PREPARING_REVIEWS: "生成审核事项", PUBLISHING: "兼容旧任务发布",
};

const outcomeLabels: Record<PegasusItem["executionState"], string> = {
  PENDING: "等待处理", COPYING: "复制内容", VALIDATING: "运行检查", PUBLISHING: "正在发布", PUBLISHED: "已发布",
  REVIEW_PENDING: "待管理员审核", REVIEW_DISCARDED: "审核已丢弃",
  SKIPPED_EXISTING: "内容已存在", SKIPPED_MAPPING: "集合已跳过", BLOCKED_SOURCE: "源文件阻断", BLOCKED_CONTENT: "内容阻断",
  BLOCKED_VALIDATION: "运行检查阻断", SOURCE_CHANGED: "源文件已变化", READ_FAILED: "读取失败", COMMIT_FAILED: "提交失败", CANCELLED: "已取消",
};

const runtimeReasonCatalog: Record<string, { title: string; explanation: string; action: string }> = {
  LAUNCH_BIOS_MISSING: { title: "缺少运行所需的 BIOS / 基础 ROM", explanation: "当前核心要求的 BIOS 或 Arcade 基础归档没有安装，运行检查无法组成完整启动内容。", action: "前往 BIOS 管理安装缺失文件，再在本任务中重新运行检查。" },
  LAUNCH_PARENT_MISSING: { title: "缺少父 ROM", explanation: "这是 split ROM，当前 ZIP 只包含子机差异，仍需要 DAT 指定的 parent archive。", action: "把缺失的父 ROM ZIP 放入同一 Pegasus 来源并声明到相同目标目录，然后重新运行检查。" },
  ARCADE_CONTENT_MISSING_ENTRY: { title: "ROM ZIP 缺少必要条目", explanation: "ZIP 文件名可以识别，但归档内部没有包含当前活动 DAT 要求的全部 ROM 条目。", action: "换用与当前核心和 DAT 版本匹配的完整 ROM ZIP。" },
  ARCADE_DEPENDENCY_MISMATCH: { title: "父 ROM 或 BIOS 内容不匹配", explanation: "依赖归档存在，但其中的文件名、大小或校验信息与当前活动 DAT 不一致。", action: "替换为与当前核心和 DAT 版本匹配的依赖归档。" },
  UNSUPPORTED_MERGED_ROMSET: { title: "当前不支持 merged ROM set", explanation: "该归档需要从 merged ROM set 中拆分依赖，当前自动准备流程无法安全构造可审核的运行内容。", action: "改用 split 或 non-merged ROM set 后重新导入。" },
  UNSUPPORTED_CHD: { title: "当前核心不支持此 CHD 组合", explanation: "DAT 识别到了 CHD 依赖，但当前核心的导入能力无法装配这种内容。", action: "换用该核心支持的 ROM set，或映射到支持此内容的游戏目录。" },
  ARCADE_DAT_UNAVAILABLE: { title: "Arcade DAT 不可用", explanation: "目标核心没有可用的活动 DAT，无法判断 ROM 和依赖闭包。", action: "先在 DAT 版本管理中修复或激活对应 DAT，再重新运行检查。" },
  ARCADE_DEPENDENCY_CYCLE: { title: "DAT 依赖关系存在循环", explanation: "当前 DAT 中这台 machine 的 parent / BIOS 依赖形成循环，无法构造有限运行闭包。", action: "更换或修复目标核心的 DAT 版本。" },
  MULTI_DISC_FILE_MISSING: { title: "多盘游戏缺少引用文件", explanation: "M3U 引用的光盘文件没有全部出现在冻结的来源快照中。", action: "补齐列出的光盘文件，并保持 M3U 中的相对路径一致。" },
  LAUNCH_CORE_VALIDATION_UNAVAILABLE: { title: "核心运行检查不可用", explanation: "核心依赖或验证器当前无法完成检查。", action: "确认核心 artifact、DAT 和 BIOS 状态后重新运行检查。" },
  PEGASUS_LIBRARY_IMPORT_FAILED: { title: "内部导入检查未完成", explanation: "内容已经复制，但复用游戏入库检查时发生了可重试错误。", action: "点击页面顶部的“重新运行检查”重试，不需要重新扫描目录。" },
  PEGASUS_RUNTIME_BLOCKED: { title: "旧任务未保留具体诊断", explanation: "这条记录由旧逻辑统一写成运行检查阻断，原始具体原因没有保存到 Pegasus 结果中。", action: "点击页面顶部的“重新运行检查”，系统会保留新的具体诊断。" },
};

const failureReasonCatalog: Record<string, { title: string; explanation: string; action: string }> = {
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

function runtimeReason(item: PegasusItem) {
  if (item.executionState === "REVIEW_PENDING" && item.runtimeCheck?.status === "READY" && !item.errorCode) return null;
  const code = item.runtimeCheck?.code ?? item.errorCode ?? item.discoveryCode;
  if (!code) return null;
  const failureReason = item.failureDetails ? failureReasonCatalog[item.failureDetails.causeCode] : null;
  if (failureReason) return { code, ...failureReason };
  return { code, ...(runtimeReasonCatalog[code] ?? { title: "处理被阻断", explanation: "服务端返回了稳定诊断码，请结合下面的检查证据处理。", action: "按缺失文件和依赖信息修正来源后重新导入或重试。" }) };
}

function RuntimeCheckDetails({ item }: { item: PegasusItem }) {
  const reason = runtimeReason(item);
  if (!reason || item.executionState === "PUBLISHED" || item.executionState === "SKIPPED_EXISTING") return null;
  const check = item.runtimeCheck;
  const failure = item.failureDetails;
  return <details className="pegasus-runtime-diagnostic"><summary>查看具体原因与处理建议</summary><div className="pegasus-runtime-diagnostic-body">
    <header><div><strong>{reason.title}</strong><p>{reason.explanation}</p></div><code>{reason.code}</code></header>
    {failure ? <section className="pegasus-internal-diagnostic" aria-label="内部排查信息"><h4>内部排查信息</h4><dl>
      <div><dt>失败阶段</dt><dd><code>{failure.stage}</code></dd></div>
      <div><dt>内部操作</dt><dd><code>{failure.operation}</code></dd></div>
      <div><dt>底层原因分类</dt><dd><code>{failure.causeCode}</code></dd></div>
      <div><dt>Pegasus Item ID</dt><dd><code>{item.id}</code></dd></div>
      {failure.relativePath ? <div><dt>来源相对路径</dt><dd><code>{failure.relativePath}</code></dd></div> : null}
      {failure.observedFileCount !== null ? <div><dt>组装文件数量</dt><dd><code>{failure.observedFileCount}{failure.allowedFileCount !== null ? ` / 上限 ${failure.allowedFileCount}` : ""}</code></dd></div> : null}
      {failure.libraryImportJobId ? <div><dt>内部 ImportJob</dt><dd><code>{failure.libraryImportJobId}</code></dd></div> : null}
      {failure.libraryImportItemId ? <div><dt>内部 ImportItem</dt><dd><code>{failure.libraryImportItemId}</code></dd></div> : null}
    </dl><div className="pegasus-technical-detail"><strong>技术详情</strong><code>{failure.technicalDetail || "服务端没有返回额外文本；请使用阶段、操作和原因码定位。"}</code></div></section> : null}
    <dl>{check?.coreId ? <div><dt>检查核心</dt><dd>{check.coreName || check.coreId} <code>{check.coreId}</code></dd></div> : null}{check?.machine ? <div><dt>识别 machine</dt><dd><code>{check.machine}</code></dd></div> : null}{check?.missingEntries.length ? <div><dt>缺失文件 / 条目</dt><dd>{check.missingEntries.map((entry) => <code key={entry}>{entry}</code>)}</dd></div> : null}{check?.mismatchedEntries.length ? <div><dt>不匹配条目</dt><dd>{check.mismatchedEntries.map((entry) => <code key={entry}>{entry}</code>)}</dd></div> : null}{check?.missingDiscs.length ? <div><dt>缺失光盘</dt><dd>{check.missingDiscs.map((disc) => <code key={`${disc.ordinal}-${disc.sourceReference}`}>Disc {disc.ordinal}: {disc.sourceReference}</code>)}</dd></div> : null}</dl>
    {check?.dependencies.length ? <div className="pegasus-runtime-dependencies"><h4>依赖明细</h4>{check.dependencies.map((dependency) => <article key={`${dependency.kind}-${dependency.machine}`}><p><strong>{dependency.kind === "PARENT" ? "父 ROM" : "BIOS / 基础 ROM"}</strong><code>{dependency.expectedLogicalName || `${dependency.machine}.zip`}</code><span>{dependency.state}</span></p>{dependency.requiredBy ? <small>由 {dependency.requiredBy} 依赖</small> : null}{dependency.requiredEntries.length ? <details><summary>查看 {dependency.requiredEntries.length} 个必需条目</summary><code>{dependency.requiredEntries.join(" · ")}</code></details> : null}</article>)}</div> : null}
    {check?.bios.length ? <div className="pegasus-runtime-dependencies"><h4>BIOS 明细</h4>{check.bios.map((bios) => <article key={bios.logicalName}><p><strong>{bios.requirementMode}</strong><code>{bios.logicalName}</code><span>{bios.installationStatus ?? "未安装"}</span></p></article>)}</div> : null}
    <p className="pegasus-runtime-action"><strong>处理建议</strong>{reason.action}{reason.code === "LAUNCH_BIOS_MISSING" ? <Link href="/admin/bios">打开 BIOS 管理 →</Link> : null}</p>
  </div></details>;
}

export function pegasusStateTone(state: PegasusImportSummary["state"]): "good" | "warn" | "bad" | "info" {
  if (state === "COMPLETED") return "good";
  if (state === "FAILED" || state === "PARTIAL_FAILURE") return "bad";
  if (state === "CANCELLED" || state === "CANCEL_REQUESTED" || state === "EXPIRED") return "warn";
  return "info";
}

function message(response: Response, fallback: string) {
  return responseError(response, fallback);
}

type MappingDraft = { action: "" | "IMPORT" | "SKIP"; platformInstanceId: string; tags: TagReference[] };

function mergeTags(current: TagReference[], additions: TagReference[]) {
  const merged = new Map(current.map((tag) => [tag.tagId, tag]));
  additions.forEach((tag) => merged.set(tag.tagId, tag));
  return [...merged.values()].sort((left, right) => left.name.localeCompare(right.name, "zh-CN"));
}

export function PegasusImportDrawer({ open, roots, platformInstances, activeTags = [], resumablePlan, onClose, onStarted }: {
  open: boolean;
  roots: ServerImportRoot[];
  platformInstances: PegasusPlatformInstance[];
  activeTags?: TagReference[];
  resumablePlan?: PegasusImportSummary;
  onClose: () => void;
  onStarted: (summary: PegasusImportSummary) => void;
}) {
  const router = useRouter();
  const drawer = useRef<HTMLElement>(null);
  const closeButton = useRef<HTMLButtonElement>(null);
  const hydratedPlanId = useRef("");
  const refreshRequest = useRef<{ planId: string; promise: Promise<PegasusImportSummary> } | null>(null);
  const [step, setStep] = useState<1 | 2 | 3>(resumablePlan ? 2 : 1);
  const [rootId, setRootId] = useState(resumablePlan?.root.id ?? roots.find((root) => root.status === "AVAILABLE")?.id ?? "");
  const [path, setPath] = useState(resumablePlan?.sourceRelativePath ?? "");
  const [directories, setDirectories] = useState<Directory[]>([]);
  const [directoryCursor, setDirectoryCursor] = useState<string | null>(null);
  const [directoryLoading, setDirectoryLoading] = useState(false);
  const [plan, setPlan] = useState<PegasusImportSummary | null>(resumablePlan ?? null);
  const [collections, setCollections] = useState<PegasusCollection[]>([]);
  const [mappings, setMappings] = useState<Record<string, MappingDraft>>({});
  const [batchTags, setBatchTags] = useState<TagReference[]>([]);
  const [batchTagStatus, setBatchTagStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const availableInstances = useMemo(() => platformInstances.filter((instance) => instance.enabled), [platformInstances]);
  const breadcrumbs = useMemo(() => path ? path.split("/") : [], [path]);
  const selectedRoot = roots.find((root) => root.id === rootId);

  const loadCollections = useCallback(async (planId: string) => {
    const all: PegasusCollection[] = [];
    let cursor: string | undefined;
    do {
      const { data, response } = await api.GET("/api/v1/admin/pegasus-imports/{pegasusImportId}/collections", {
        params: { path: { pegasusImportId: planId }, query: { cursor, limit: 100 } },
      });
      if (!data) throw new Error(await message(response, "Pegasus 集合读取失败"));
      all.push(...data.items);
      cursor = data.nextCursor ?? undefined;
    } while (cursor);
    setCollections(all);
    setBatchTags([]);
    setBatchTagStatus("");
    setMappings(Object.fromEntries(all.map((collection) => [collection.id, {
      action: collection.mappingAction ?? "",
      platformInstanceId: collection.targetPlatformInstanceId ?? "",
      tags: collection.tagSnapshot ?? [],
    }])));
    return all;
  }, []);

  const refreshPlan = useCallback((planId: string) => {
    if (refreshRequest.current?.planId === planId) return refreshRequest.current.promise;
    const promise = (async () => {
      const { data, response } = await api.GET("/api/v1/admin/pegasus-imports/{pegasusImportId}", { params: { path: { pegasusImportId: planId } } });
      if (!data) throw new Error(await message(response, "Pegasus 计划读取失败"));
      setPlan(data);
      if (data.state === "AWAITING_MAPPING") {
        const loaded = await loadCollections(data.id);
        const complete = loaded.length > 0 && loaded.every((collection) => collection.mappingAction === "SKIP" || collection.mappingAction === "IMPORT" && Boolean(collection.targetPlatformInstanceId));
        setStep(complete ? 3 : 2);
      }
      return data;
    })();
    refreshRequest.current = { planId, promise };
    void promise.then(
      () => { if (refreshRequest.current?.promise === promise) refreshRequest.current = null; },
      () => { if (refreshRequest.current?.promise === promise) refreshRequest.current = null; },
    );
    return promise;
  }, [loadCollections]);

  useEffect(() => {
    if (!open) return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const root = document.documentElement;
    const body = document.body;
    const previousRootOverflow = root.style.overflow;
    const previousBodyOverflow = body.style.overflow;
    const previousBodyPaddingRight = body.style.paddingRight;
    const scrollbarWidth = root.clientWidth > 0 ? Math.max(0, window.innerWidth - root.clientWidth) : 0;
    const bodyPaddingRight = Number.parseFloat(window.getComputedStyle(body).paddingRight) || 0;
    root.style.overflow = "hidden";
    body.style.overflow = "hidden";
    if (scrollbarWidth > 0) body.style.paddingRight = `${bodyPaddingRight + scrollbarWidth}px`;
    closeButton.current?.focus({ preventScroll: true });
    return () => {
      root.style.overflow = previousRootOverflow;
      body.style.overflow = previousBodyOverflow;
      body.style.paddingRight = previousBodyPaddingRight;
      if (previous?.isConnected) previous.focus({ preventScroll: true });
    };
  }, [open]);

  const resumablePlanId = resumablePlan?.id ?? "";
  useEffect(() => {
    if (!open) {
      hydratedPlanId.current = "";
      return;
    }
    if (!resumablePlanId || hydratedPlanId.current === resumablePlanId) return;
    let active = true;
    queueMicrotask(() => {
      if (!active) return;
      hydratedPlanId.current = resumablePlanId;
      void refreshPlan(resumablePlanId).catch((caught: unknown) => setError(caught instanceof Error ? caught.message : "Pegasus 计划读取失败"));
    });
    return () => { active = false; };
  }, [open, refreshPlan, resumablePlanId]);

  useEffect(() => {
    if (!open || step !== 1 || !rootId) return;
    const controller = new AbortController();
    queueMicrotask(() => { if (!controller.signal.aborted) setDirectoryLoading(true); });
    void api.GET("/api/v1/admin/server-import-roots/{rootId}/directories", {
      params: { path: { rootId }, query: { path, limit: 100 } }, signal: controller.signal,
    }).then(async ({ data, response }) => {
      if (!data) throw new Error(await message(response, "服务器目录读取失败"));
      setDirectories(data.items); setDirectoryCursor(data.nextCursor);
    }).catch((caught: unknown) => {
      if (!(caught instanceof DOMException && caught.name === "AbortError")) setError(caught instanceof Error ? caught.message : "服务器目录读取失败");
    }).finally(() => setDirectoryLoading(false));
    return () => controller.abort();
  }, [open, path, rootId, step]);

  useEffect(() => {
    if (!open || !plan || plan.state !== "SCANNING") return;
    const update = () => void refreshPlan(plan.id).catch((caught: unknown) => setError(caught instanceof Error ? caught.message : "扫描进度读取失败"));
    const timer = window.setInterval(update, 2_000);
    const source = typeof EventSource === "undefined" ? null : new EventSource(`/api/v1/admin/jobs/${encodeURIComponent(plan.scanJobId)}/events`, { withCredentials: true });
    source?.addEventListener("progress", update);
    source?.addEventListener("succeeded", update);
    source?.addEventListener("failed", update);
    return () => { window.clearInterval(timer); source?.close(); };
  }, [open, plan, refreshPlan]);

  async function loadMoreDirectories() {
    if (!directoryCursor || directoryLoading) return;
    setDirectoryLoading(true); setError("");
    try {
      const { data, response } = await api.GET("/api/v1/admin/server-import-roots/{rootId}/directories", { params: { path: { rootId }, query: { path, cursor: directoryCursor, limit: 100 } } });
      if (!data) throw new Error(await message(response, "服务器目录读取失败"));
      setDirectories((current) => [...current, ...data.items.filter((item) => !current.some((known) => known.relativePath === item.relativePath))]);
      setDirectoryCursor(data.nextCursor);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "服务器目录读取失败"); }
    finally { setDirectoryLoading(false); }
  }

  async function scan() {
    if (!rootId || selectedRoot?.status !== "AVAILABLE") return;
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/pegasus-imports", {
        params: { header: { ...writeHeaders(), "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } },
        body: { rootId, sourceRelativePath: path },
      });
      if (!data) throw new Error(await message(response, "Pegasus 扫描创建失败"));
      hydratedPlanId.current = data.id;
      setPlan(data); setStep(2); onStarted(data);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "Pegasus 扫描创建失败"); }
    finally { setBusy(false); }
  }

  async function confirmMappings() {
    if (!plan || collections.some((collection) => !mappings[collection.id]?.action) || collections.some((collection) => mappings[collection.id]?.action === "IMPORT" && !mappings[collection.id]?.platformInstanceId)) return;
    setBusy(true); setError("");
    try {
      let current = plan;
      const values = collections.map((collection) => {
        const draft = mappings[collection.id];
        return draft.action === "SKIP" ? { collectionId: collection.id, action: "SKIP" as const, tagIds: [] } : { collectionId: collection.id, action: "IMPORT" as const, platformInstanceId: draft.platformInstanceId, tagIds: draft.tags.map((tag) => tag.tagId) };
      });
      for (let offset = 0; offset < values.length; offset += 100) {
        const { data, response } = await api.PUT("/api/v1/admin/pegasus-imports/{pegasusImportId}/collection-mappings", {
          params: { path: { pegasusImportId: current.id }, header: { ...writeHeaders(), "If-Match": `"v${current.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } },
          body: { mappings: values.slice(offset, offset + 100) },
        });
        if (!data) throw new Error(await message(response, "集合映射保存失败"));
        current = data;
      }
      setPlan(current); setStep(3);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "集合映射保存失败"); }
    finally { setBusy(false); }
  }

  function applyBatchTags() {
    if (!batchTags.length) return;
    const targets = collections.filter((collection) => mappings[collection.id]?.action !== "SKIP");
    const oversized = targets.find((collection) => mergeTags(mappings[collection.id]?.tags ?? [], batchTags).length > 20);
    if (oversized) {
      setError(`无法批量添加：${oversized.name} 合并后会超过 20 个标签。请先移除部分标签。`);
      return;
    }
    setMappings((current) => {
      const next = { ...current };
      targets.forEach((collection) => {
        const draft = current[collection.id] ?? { action: "", platformInstanceId: "", tags: [] };
        next[collection.id] = { ...draft, tags: mergeTags(draft.tags, batchTags) };
      });
      return next;
    });
    const gameCount = targets.reduce((total, collection) => total + collection.gameCount, 0);
    setBatchTagStatus(`已追加到 ${targets.length} 个未跳过 Collection，覆盖 ${gameCount} 个游戏；仍可在下方逐项调整。`);
  }

  async function startImport() {
    if (!plan) return;
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/pegasus-imports/{pegasusImportId}/start", {
        params: { path: { pegasusImportId: plan.id }, header: { ...writeHeaders(), "If-Match": `"v${plan.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } },
        body: { version: plan.version },
      });
      if (!data) throw new Error(await message(response, "Pegasus 导入启动失败"));
      onStarted(data); onClose(); router.push(`/admin/imports/server/pegasus/${data.id}`);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "Pegasus 导入启动失败"); }
    finally { setBusy(false); }
  }

  const mapped = Object.values(mappings).filter((mapping) => mapping.action === "IMPORT").length;
  const skipped = Object.values(mappings).filter((mapping) => mapping.action === "SKIP").length;
  const taggedCollections = collections.filter((collection) => mappings[collection.id]?.action === "IMPORT" && mappings[collection.id]?.tags.length);
  const taggedGames = taggedCollections.reduce((total, collection) => total + collection.gameCount, 0);
  const mappedTags = mergeTags([], taggedCollections.flatMap((collection) => mappings[collection.id]?.tags ?? []));
  const mappingComplete = collections.length > 0 && mapped + skipped === collections.length && collections.every((collection) => mappings[collection.id]?.action !== "IMPORT" || Boolean(mappings[collection.id]?.platformInstanceId));
  if (!open) return null;
  return <><button type="button" className="runtime-drawer-backdrop" aria-label="关闭 Pegasus 导入" disabled={busy} onClick={onClose} /><aside ref={drawer} className="runtime-drawer server-import-drawer pegasus-import-drawer" role="dialog" aria-modal="true" aria-labelledby="pegasus-import-title" onKeyDown={(event) => {
    if (event.key === "Escape" && !busy) { event.preventDefault(); onClose(); }
    if (event.key !== "Tab") return;
    const focusable = Array.from(drawer.current?.querySelectorAll<HTMLElement>("button:not(:disabled),input:not(:disabled),select:not(:disabled),a[href],[tabindex]:not([tabindex='-1'])") ?? []);
    if (!focusable.length) return;
    const first = focusable[0]; const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  }}><header><div><StatusBadge tone="info">Pegasus ROM</StatusBadge><h2 id="pegasus-import-title">从 Pegasus 目录准备审核事项</h2><p>只显示允许 root 内的相对目录；扫描不会复制 ROM 或创建游戏。</p></div><button ref={closeButton} type="button" className="runtime-drawer-close" aria-label="关闭" disabled={busy} onClick={onClose}><AppIcon name="x" /></button></header>
    <ol className="pegasus-stepper" aria-label="导入步骤"><li className={step === 1 ? "is-active" : step > 1 ? "is-complete" : ""}><span>1</span>选择目录</li><li className={step === 2 ? "is-active" : step > 2 ? "is-complete" : ""}><span>2</span>检查与映射</li><li className={step === 3 ? "is-active" : ""}><span>3</span>确认审核计划</li></ol>
    <div className="runtime-drawer-body">
      {step === 1 ? <><fieldset className="server-root-options"><legend>服务器位置</legend>{roots.map((root) => <label key={root.id}><input type="radio" name="pegasus-root" checked={rootId === root.id} disabled={busy || root.status !== "AVAILABLE"} onChange={() => { setRootId(root.id); setPath(""); }} /><span><strong>{root.label}</strong><small>{root.status === "AVAILABLE" ? "可用" : "不可用"}</small></span></label>)}</fieldset><div className="server-directory-browser"><nav aria-label="当前目录"><button type="button" onClick={() => setPath("")} disabled={!path || busy}>根目录</button>{breadcrumbs.map((part, index) => <button type="button" key={`${part}-${index}`} disabled={index === breadcrumbs.length - 1 || busy} onClick={() => setPath(breadcrumbs.slice(0, index + 1).join("/"))}>/ {part}</button>)}</nav>{directories.length ? <><ul>{directories.map((directory) => <li key={directory.relativePath}><button type="button" disabled={busy} onClick={() => setPath(directory.relativePath)}><AppIcon name="folder" /><span>{directory.name}</span><span aria-hidden="true">→</span></button></li>)}</ul>{directoryCursor ? <button type="button" className="button secondary compact" disabled={directoryLoading || busy} onClick={() => void loadMoreDirectories()}>{directoryLoading ? "正在读取…" : "加载更多目录"}</button> : null}</> : <p role="status">{directoryLoading ? "正在读取子目录…" : "当前目录没有可进入的子目录。"}</p>}</div><div className="server-import-selection-summary"><strong>{selectedRoot?.label ?? "未选择"} / {path || "根目录"}</strong><span>先异步读取 metadata、文件大小与稳定 facts；确认映射后才读取完整 ROM bytes。</span></div></> : null}
      {step === 2 ? <>{plan?.state === "SCANNING" ? <div className="pegasus-scan-progress" aria-live="polite"><span className="button-spinner" /><h3>{plan.phase ? phaseLabels[plan.phase] : "扫描准备中"}</h3><p>任务离开页面后仍会继续。当前发现 {plan.counts.metadata} 个 metadata、{plan.counts.collections} 个集合、{plan.counts.games} 个游戏。</p></div> : null}{plan?.state === "FAILED" ? <div className="runtime-inline-empty"><h3>扫描未完成</h3><p>{plan.lastErrorCode ?? "扫描任务失败"}</p></div> : null}{plan?.state === "AWAITING_MAPPING" ? <><div className="pegasus-scan-summary"><div><span>Metadata</span><strong>{plan.counts.metadata}</strong></div><div><span>Collection</span><strong>{plan.counts.collections}</strong></div><div><span>Game</span><strong>{plan.counts.games}</strong></div><div><span>发现视频</span><strong>{plan.counts.videos}</strong></div></div><p className="pegasus-mapping-note">每个 source collection 必须明确选择游戏目录或跳过；Retrom 不会根据名称、扩展名或 launch 命令猜测。</p><section className="pegasus-batch-tags" aria-labelledby="pegasus-batch-tags-title"><header><div><h3 id="pegasus-batch-tags-title">批量添加默认标签</h3><p>选择一次后追加到所有未跳过 Collection，不覆盖已有选择；下方仍可逐项增删。</p></div><span>{collections.reduce((total, collection) => total + collection.gameCount, 0)} 个游戏</span></header><TagPicker label="批次标签" options={activeTags} selected={batchTags} disabled={busy} onChange={(tags) => { setBatchTags(tags); setBatchTagStatus(""); }} description="标签必须先在标签管理中建立。点击应用后，尚未选择处理方式的 Collection 也会保留这些默认标签。" /><div className="pegasus-batch-tag-actions"><button type="button" className="button secondary compact" disabled={busy || !batchTags.length} onClick={applyBatchTags}>应用到所有未跳过 Collection</button>{batchTagStatus ? <p role="status">{batchTagStatus}</p> : null}</div></section><div className="pegasus-collection-list">{collections.map((collection) => { const draft = mappings[collection.id] ?? { action: "" as const, platformInstanceId: "", tags: [] }; return <article key={collection.id}><div><h3>{collection.name}</h3><p>{collection.metadataRelativePath} · segment {collection.segmentOrdinal + 1}</p><small>{collection.shortName ? `shortname: ${collection.shortName} · ` : ""}{collection.gameCount} 个游戏 · {collection.issueCount} 个阻断/问题</small></div><label><span>处理方式</span><select aria-label={`${collection.name} 处理方式`} value={draft.action === "SKIP" ? "SKIP" : draft.platformInstanceId ? `IMPORT:${draft.platformInstanceId}` : ""} onChange={(event) => { const value = event.target.value; setMappings((current) => ({ ...current, [collection.id]: value === "SKIP" ? { action: "SKIP", platformInstanceId: "", tags: [] } : value.startsWith("IMPORT:") ? { action: "IMPORT", platformInstanceId: value.slice(7), tags: current[collection.id]?.tags ?? [] } : { action: "", platformInstanceId: "", tags: [] } })); }}><option value="">请选择，不会自动映射</option><option value="SKIP">跳过此集合</option>{availableInstances.map((instance) => <option value={`IMPORT:${instance.id}`} key={instance.id}>导入到 {instance.name} · {instance.defaultCoreName}</option>)}</select></label>{draft.action === "IMPORT" ? <div className="pegasus-collection-tags"><TagPicker label={`${collection.name} 的默认标签`} options={activeTags} selected={draft.tags} disabled={busy} onChange={(tags) => setMappings((current) => ({ ...current, [collection.id]: { ...draft, tags } }))} description="此集合生成的每个待审核游戏都会继承这些标签。" /></div> : null}</article>; })}</div></> : null}</> : null}
      {step === 3 && plan ? <><div className="pegasus-review-table"><div><span>来源</span><strong>{plan.root.label} / {plan.sourceRelativePath || "根目录"}</strong></div><div><span>映射</span><strong>{mapped} 个处理 · {skipped} 个跳过</strong></div><div><span>默认标签覆盖</span><strong>{taggedCollections.length} 个 Collection · {taggedGames} 个游戏</strong></div><div><span>可处理 / 源内容阻断</span><strong>{plan.counts.processable} / {plan.counts.blocked} 个游戏</strong></div><div><span>封面 / 视频</span><strong>{plan.counts.covers} / {plan.counts.videos}</strong></div><div><span>预计最多读取</span><strong>{formatBytes(plan.counts.estimatedSourceBytes)}</strong></div><div><span>发布方式</span><strong>全部进入待审核，由管理员逐项决定</strong></div></div>{mappedTags.length ? <div className="pegasus-review-tags"><span>本批使用的标签</span><TagChips tags={mappedTags} /></div> : null}<p className="pegasus-mapping-note">开始时会重新核对 metadata digest 与源文件 facts。后台只准备来源与运行检查，不会创建游戏；已经生成的审核事项在取消任务后仍会保留。</p></> : null}
    </div><footer><button type="button" className="button secondary" disabled={busy} onClick={onClose}>关闭</button>{step === 1 ? <button type="button" className="button" disabled={busy || !rootId || selectedRoot?.status !== "AVAILABLE"} onClick={() => void scan()}>{busy ? "正在创建…" : "扫描此目录"}</button> : null}{step === 2 && plan?.state === "AWAITING_MAPPING" ? <button type="button" className="button" disabled={busy || !mappingComplete} onClick={() => void confirmMappings()}>{busy ? "正在保存…" : "确认映射"}</button> : null}{step === 3 ? <button type="button" className="button" disabled={busy} onClick={() => void startImport()}>{busy ? "正在启动…" : "开始准备审核事项"}</button> : null}</footer></aside><Toast toast={error ? { message: error, tone: "bad" } : null} onDismiss={() => setError("")} /></>;
}

type DetailFilters = { query: string; outcome: string; warning: string; collectionId: string };

export function PegasusImportDetailManager({ initialSummary, initialItems, collections, roots, platformInstances, activeTags = [], initialFilters }: {
  initialSummary: PegasusImportSummary;
  initialItems: PegasusItemList;
  collections: PegasusCollection[];
  roots: ServerImportRoot[];
  platformInstances: PegasusPlatformInstance[];
  activeTags?: TagReference[];
  initialFilters: DetailFilters;
}) {
  const [summary, setSummary] = useState(initialSummary);
  const [items, setItems] = useState(initialItems.items);
  const [nextCursor, setNextCursor] = useState(initialItems.nextCursor);
  const [filters, setFilters] = useState(initialFilters);
  const [draft, setDraft] = useState(initialFilters);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [cancelOpen, setCancelOpen] = useState(false);
  const [mappingOpen, setMappingOpen] = useState(false);

  const requestSummary = useCallback(async () => {
    const { data, response } = await api.GET("/api/v1/admin/pegasus-imports/{pegasusImportId}", { params: { path: { pegasusImportId: initialSummary.id } } });
    if (!data) throw new Error(await message(response, "任务摘要读取失败"));
    setSummary(data);
  }, [initialSummary.id]);

  const requestItems = useCallback(async (active: DetailFilters, cursor?: string, append = false) => {
    const { data, response } = await api.GET("/api/v1/admin/pegasus-imports/{pegasusImportId}/items", { params: { path: { pegasusImportId: initialSummary.id }, query: { q: active.query || undefined, outcome: active.outcome || undefined, warning: active.warning || undefined, collectionId: active.collectionId || undefined, cursor, limit: 50 } } });
    if (!data) throw new Error(await message(response, "任务结果读取失败"));
    setItems((current) => append ? [...current, ...data.items.filter((item) => !current.some((known) => known.id === item.id))] : data.items);
    setNextCursor(data.nextCursor);
  }, [initialSummary.id]);

  useEffect(() => {
    const values = new URLSearchParams(window.location.search);
    for (const [name, value] of Object.entries({ q: filters.query, outcome: filters.outcome, warning: filters.warning, collectionId: filters.collectionId })) {
      if (value) values.set(name, value); else values.delete(name);
    }
    const encoded = values.toString();
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${encoded ? `?${encoded}` : ""}`);
  }, [filters]);

  useEffect(() => {
    if (!["SCANNING", "QUEUED", "RUNNING", "CANCEL_REQUESTED"].includes(summary.state)) return;
    const update = () => { void requestSummary().catch(() => undefined); void requestItems(filters).catch(() => undefined); };
    const timer = window.setInterval(update, 4_000);
    const jobId = summary.importJobId ?? summary.scanJobId;
    const source = typeof EventSource === "undefined" ? null : new EventSource(`/api/v1/admin/jobs/${encodeURIComponent(jobId)}/events`, { withCredentials: true });
    for (const event of ["progress", "succeeded", "failed", "cancelled"]) source?.addEventListener(event, update);
    return () => { window.clearInterval(timer); source?.close(); };
  }, [filters, requestItems, requestSummary, summary.importJobId, summary.scanJobId, summary.state]);

  async function applyFilters() {
    setBusy(true); setError("");
    try { setFilters(draft); await requestItems(draft); }
    catch (caught) { setError(caught instanceof Error ? caught.message : "任务结果读取失败"); }
    finally { setBusy(false); }
  }

  async function cancel() {
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/pegasus-imports/{pegasusImportId}/cancel", { params: { path: { pegasusImportId: summary.id }, header: { ...writeHeaders(), "If-Match": `"v${summary.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } }, body: { reason: "管理员停止 Pegasus ROM 导入" } });
      if (!data) throw new Error(await message(response, "取消任务失败"));
      setSummary(data); setCancelOpen(false);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "取消任务失败"); }
    finally { setBusy(false); }
  }

  async function retry() {
    setBusy(true); setError("");
    try {
      const { data, response } = await api.POST("/api/v1/admin/pegasus-imports/{pegasusImportId}/retry", { params: { path: { pegasusImportId: summary.id }, header: { ...writeHeaders(), "If-Match": `"v${summary.version}"`, "Idempotency-Key": newUuid(), "X-Retrom-Csrf": "" } }, body: {} });
      if (!data) throw new Error(await message(response, "重试任务失败"));
      setSummary(data);
    } catch (caught) { setError(caught instanceof Error ? caught.message : "重试任务失败"); }
    finally { setBusy(false); }
  }

  const terminal = ["COMPLETED", "PARTIAL_FAILURE", "FAILED", "CANCELLED", "EXPIRED"].includes(summary.state);
  const reviewURL = `/admin/reviews?pegasusImportId=${encodeURIComponent(summary.id)}`;
  return <div className="server-import-detail-page pegasus-detail-page">
    <section className="server-import-detail-head panel"><div><StatusBadge tone={pegasusStateTone(summary.state)}>{pegasusStateLabels[summary.state]}</StatusBadge><h2>{summary.root.label} / {summary.sourceRelativePath || "根目录"}</h2><p aria-live="polite">{summary.phase ? phaseLabels[summary.phase] : terminal ? summary.counts.reviewPending ? `已准备 ${summary.counts.reviewPending} 个待审核游戏` : "后台准备任务已结束" : "等待处理"}</p></div><div>{["SCANNING", "QUEUED", "RUNNING"].includes(summary.state) ? <button type="button" className="button secondary" disabled={busy} onClick={() => setCancelOpen(true)}>取消任务</button> : null}{summary.retryable ? <button type="button" className="button secondary" disabled={busy} onClick={() => void retry()}>重试失败条目</button> : null}{summary.counts.reviewPending ? <Link href={reviewURL} className="button">逐项审核 {summary.counts.reviewPending} 个游戏</Link> : null}{summary.state === "AWAITING_MAPPING" ? <button type="button" className="button" disabled={busy} onClick={() => setMappingOpen(true)}>继续映射</button> : <Link href="/admin/imports/server?action=pegasus" className="button secondary">新建 Pegasus 导入</Link>}</div></section>
    {summary.counts.reviewPending ? <section className="pegasus-review-callout panel"><div><span>下一步 · 人工审核</span><h2>内容已准备好，但尚未进入游戏库</h2><p>请逐条核对运行检查、标题、封面和视频。只有点击“通过并发布”的游戏才会出现在游戏库；系统不提供批量通过。</p></div><Link href={reviewURL} className="button">打开这批审核队列 →</Link></section> : null}
    <section className="runtime-kpis" aria-label="Pegasus 导入摘要"><article><small>扫描范围</small><strong>{summary.counts.games}</strong><p>{summary.counts.collections} 个 Collection · {summary.counts.processable} 项可处理</p></article><article className={summary.counts.reviewPending ? "has-warning" : ""}><small>等待逐项审核</small><strong>{summary.counts.reviewPending}</strong><p>不会自动发布到游戏库</p></article><article className="has-success"><small>已发布 / 已丢弃 / 已存在</small><strong>{summary.counts.published} / {summary.counts.reviewDiscarded} / {summary.counts.existing}</strong><p>均保留来源与审核证据</p></article><article className={summary.counts.blocked + summary.counts.failed ? "has-danger" : ""}><small>源内容阻断 / 任务失败</small><strong>{summary.counts.blocked} / {summary.counts.failed}</strong><p>{summary.counts.mediaWarnings} 个媒体警告</p></article></section>
    {summary.lastErrorCode ? <p className="server-import-error panel"><strong>{summary.lastErrorCode}</strong><span>外部 source 不属于备份；目录变化时请按结果提示重扫或重试。</span></p> : null}
    <section className="server-import-results"><div className="runtime-section-heading"><div><h2>准备与审核结果</h2><p>后台只准备审核事项；待审核条目必须由管理员逐项决定是否发布。</p></div><span>{items.length} / {summary.counts.games} 项</span></div>
      <form className="server-import-result-filters panel pegasus-result-filters" onSubmit={(event) => { event.preventDefault(); void applyFilters(); }}><label><span>搜索标题</span><input type="search" value={draft.query} onChange={(event) => setDraft((current) => ({ ...current, query: event.target.value }))} /></label><label><span>结果</span><select value={draft.outcome} onChange={(event) => setDraft((current) => ({ ...current, outcome: event.target.value }))}><option value="">全部结果</option>{Object.entries(outcomeLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label><label><span>媒体警告</span><input value={draft.warning} placeholder="例如 PEGASUS_VIDEO_UNSUPPORTED" onChange={(event) => setDraft((current) => ({ ...current, warning: event.target.value }))} /></label><label><span>Collection</span><select value={draft.collectionId} onChange={(event) => setDraft((current) => ({ ...current, collectionId: event.target.value }))}><option value="">全部 Collection</option>{collections.map((collection) => <option value={collection.id} key={collection.id}>{collection.name}</option>)}</select></label><button type="submit" className="button secondary compact" disabled={busy}>{busy ? "正在筛选…" : "应用筛选"}</button></form>
      <div className="pegasus-result-table" role="table" aria-label="Pegasus 导入结果">{items.map((item) => { const reason = runtimeReason(item); const reviewHref = item.reviewItemId ? `/admin/reviews/${item.reviewItemId}?returnTo=${encodeURIComponent(reviewURL)}` : null; return <article role="row" key={item.id}><div role="cell"><h3>{item.title}</h3><TagChips tags={item.tags} limit={2} ariaLabel={`${item.title} 的标签`} /><p>{item.collectionName ?? "无有效 Collection"} → {item.targetPlatformInstanceName ?? "未映射"}</p><small>{item.metadataRelativePath} · {item.contentKind ?? "内容类型待定"}</small></div><div role="cell" className="pegasus-result-media"><StatusBadge tone={item.media.cover === "READY" ? "good" : item.media.cover === "WARNING" ? "warn" : "info"}>封面 {item.media.cover}</StatusBadge><StatusBadge tone={item.media.video === "READY" ? "good" : item.media.video === "WARNING" ? "warn" : "info"}>视频 {item.media.video}</StatusBadge></div><div role="cell"><StatusBadge tone={item.executionState === "PUBLISHED" ? "good" : item.executionState === "REVIEW_PENDING" ? "info" : item.executionState.startsWith("BLOCKED") || ["SOURCE_CHANGED", "READ_FAILED", "COMMIT_FAILED"].includes(item.executionState) ? "bad" : "warn"}>{outcomeLabels[item.executionState]}</StatusBadge><small>{reason?.title ?? item.errorCode ?? item.discoveryCode ?? (item.warnings.map((warning) => warning.code).join("、") || (item.executionState === "REVIEW_PENDING" ? "等待管理员作出审核决定" : "无附加结果码"))}</small></div><div role="cell">{reviewHref && item.executionState === "REVIEW_PENDING" ? <Link className="button compact" href={reviewHref}>{item.runtimeCheck?.status === "READY" ? "审核并决定" : "处理运行问题"}</Link> : item.publishedGameId ? <Link href={`/games/${item.publishedGameId}`}>查看游戏 →</Link> : item.existingGameId ? <Link href={`/games/${item.existingGameId}`}>已有游戏 →</Link> : item.executionState === "REVIEW_DISCARDED" ? <small>管理员已在审核队列中丢弃</small> : item.discoveryCode === "PEGASUS_MULTIPLE_LAUNCH_FILES_UNSUPPORTED" ? <small>Pegasus 把多个文件视为可选启动项；请整理为单文件或受支持的 Saturn M3U。</small> : <span>—</span>}</div><div role="cell" className="pegasus-runtime-diagnostic-cell"><RuntimeCheckDetails item={item} /></div></article>; })}</div>
      {nextCursor ? <button type="button" className="button secondary server-import-history-more" disabled={busy} onClick={() => { setBusy(true); void requestItems(filters, nextCursor, true).catch((caught: unknown) => setError(caught instanceof Error ? caught.message : "加载失败")).finally(() => setBusy(false)); }}>加载更多结果</button> : null}
    </section>
    <ConfirmDialog open={cancelOpen} title="取消这次 Pegasus 准备任务？" description="已经生成的审核事项会保留，尚未处理的项目会在安全检查点停止。" confirmLabel="确认取消" tone="danger" busy={busy} onCancel={() => setCancelOpen(false)} onConfirm={() => void cancel()} />
    {mappingOpen ? <PegasusImportDrawer open roots={roots} platformInstances={platformInstances} activeTags={activeTags} resumablePlan={summary} onClose={() => setMappingOpen(false)} onStarted={(updated) => setSummary(updated)} /> : null}
    <Toast toast={error ? { message: error, tone: "bad" } : null} onDismiss={() => setError("")} />
  </div>;
}
