"use client";

import { useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { EmptyState, FeedbackBanner, PageHeader } from "@/components/ui";
import { writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";
import { responseError } from "@/lib/upload";
import { formatStorageBytes, storageBarWidth, storagePercentage } from "./format";
import {
  categoryPresentation,
  excludedPresentation,
  type StorageCategory,
  type StorageSnapshot,
} from "./model";

const storageEndpoint = "/api/v1/admin/storage-analysis";
const cleanupEndpoint = "/api/v1/admin/storage-cleanups";
const fixedExcluded = Object.keys(excludedPresentation) as StorageSnapshot["excluded"];

type StorageCleanupResult = {
  scheduledFileCount: number;
  scheduledBytes: string;
  acceptedAtMs: number;
};

async function fetchSnapshot(signal?: AbortSignal): Promise<StorageSnapshot> {
  const response = await fetch(storageEndpoint, { cache: "no-store", credentials: "same-origin", signal });
  if (!response.ok) {throw new Error(await responseError(response, "无法读取容量分析"));}
  return response.json() as Promise<StorageSnapshot>;
}

async function scheduleCleanup(): Promise<StorageCleanupResult> {
  const response = await fetch(cleanupEndpoint, {
    method: "POST",
    credentials: "same-origin",
    headers: writeHeaders({ "Idempotency-Key": newUuid() }),
  });
  if (!response.ok) {throw new Error(await responseError(response, "无法开始立即清理"));}
  return response.json() as Promise<StorageCleanupResult>;
}

function ByteValue({ bytes, label, className }: { bytes: string; label: string; className?: string }) {
  return <span className={className} tabIndex={0} title={`${bytes} bytes`} aria-label={`${label}，精确值 ${bytes} bytes`}>
    {formatStorageBytes(bytes)}
  </span>;
}

function Summary({ snapshot }: { snapshot: StorageSnapshot }) {
  const items = [
    { label: "已登记文件", bytes: snapshot.totals.registeredBytes, note: `${snapshot.totals.fileCount} 个文件` },
    { label: "保留数据", bytes: snapshot.totals.retainedBytes, note: `${storagePercentage(snapshot.totals.retainedBytes, snapshot.totals.registeredBytes)} 继续保留` },
    { label: "等待回收", bytes: snapshot.totals.pendingDeleteBytes, note: `${storagePercentage(snapshot.totals.pendingDeleteBytes, snapshot.totals.registeredBytes)} 已安排删除` },
  ];
  return <section className="storage-summary" aria-label="容量总览">
    {items.map((item) => <article key={item.label}>
      <span>{item.label}</span>
      <ByteValue bytes={item.bytes} label={item.label} />
      <small>{item.note}</small>
    </article>)}
  </section>;
}

function CapacityBar({ snapshot }: { snapshot: StorageSnapshot }) {
  return <div className="storage-bar" role="img" aria-label={`已登记容量 ${formatStorageBytes(snapshot.totals.registeredBytes)}，按六个用途分类`}>
    {snapshot.categories.map((category) => <i
      aria-hidden="true"
      className={`storage-tone-${category.code.toLowerCase().replaceAll("_", "-")}`}
      key={category.code}
      style={{ width: storageBarWidth(category.bytes, snapshot.totals.registeredBytes) }}
    />)}
  </div>;
}

function CategoryCard({ category, total }: { category: StorageCategory; total: string }) {
  const presentation = categoryPresentation[category.code];
  const tone = `storage-tone-${category.code.toLowerCase().replaceAll("_", "-")}`;
  return <article className="storage-category">
    <i className={tone} aria-hidden="true" />
    <div>
      <h3>{presentation.label}</h3>
      <p>{presentation.description}</p>
    </div>
    <div className="storage-category-value">
      <ByteValue bytes={category.bytes} label={presentation.label} />
      <small>{category.fileCount} 个文件 · {storagePercentage(category.bytes, total)}</small>
    </div>
  </article>;
}

function Breakdown({ snapshot }: { snapshot: StorageSnapshot }) {
  if (snapshot.totals.registeredBytes === "0") {
    return <EmptyState title="还没有已登记的文件" description="导入游戏、安装 BIOS 或创建存档后，这里会按各类文件显示容量。" />;
  }
  return <section className="panel storage-breakdown" aria-labelledby="storage-breakdown-title">
    <div className="panel-head"><div><h2 id="storage-breakdown-title">按用途分析</h2><p>按文件的当前用途分类；内容相同的独立文件分别计量。</p></div></div>
    <div className="panel-body">
      <CapacityBar snapshot={snapshot} />
      <div className="storage-category-list">
        {snapshot.categories.map((category) => <CategoryCard key={category.code} category={category} total={snapshot.totals.registeredBytes} />)}
      </div>
    </div>
  </section>;
}

function Details({ snapshot }: { snapshot: StorageSnapshot }) {
  const saves = snapshot.details.saveStates;
  const cleanup = snapshot.details.cleanupCandidates;
  return <section className="storage-details" aria-label="文件详情">
    <article className="panel">
      <div><span>存档文件</span><strong>{saves.activeCount} 份有效 · {saves.deletedCount} 份软删除</strong></div>
      <dl>
        <div><dt>状态文件</dt><dd><ByteValue bytes={saves.stateBytes} label="存档状态文件大小" /></dd></div>
        <div><dt>截图文件</dt><dd><ByteValue bytes={saves.screenshotBytes} label="存档截图大小" /></dd></div>
      </dl>
      <p>状态文件与截图属于各自存档，已包含在上方用途分类中。</p>
    </article>
    <article className="panel">
      <div><span>删除队列</span><strong>{cleanup.fileCount} 个文件</strong></div>
      <ByteValue className="storage-detail-total" bytes={cleanup.bytes} label="清理候选大小" />
      <p>替换或移除后，所有者移除的文件会进入清理队列；后台删除文件后，已登记总量才会下降。ROM 替换会清理绑定旧内容的存档；BIOS 替换会撤销使用旧 BIOS 的启动，存档仍可使用。</p>
    </article>
  </section>;
}

function ScopeNote({ excluded = fixedExcluded }: { excluded?: StorageSnapshot["excluded"] }) {
  return <section className="storage-scope" aria-labelledby="storage-scope-title">
    <div><p className="eyebrow">统计边界</p><h2 id="storage-scope-title">仅计算已登记文件</h2><p>口径版本 <code>OWNED_FILES_V1</code>。页面不等同于磁盘占用或可用空间。</p></div>
    <ul>{excluded.map((code) => <li key={code}>{excludedPresentation[code]}</li>)}</ul>
  </section>;
}

function LoadingState() {
  return <div className="storage-loading" role="status" aria-label="正在读取容量分析">
    <i /><i /><i /><div /><div />
  </div>;
}

function useStorageAnalysis() {
  const [snapshot, setSnapshot] = useState<StorageSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [cleaning, setCleaning] = useState(false);
  const [trackingCleanup, setTrackingCleanup] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    void fetchSnapshot(controller.signal).then((value) => {
      setSnapshot(value); setError(""); setLoading(false);
    }).catch((reason: unknown) => {
      if (reason instanceof DOMException && reason.name === "AbortError") {return;}
      setError(reason instanceof Error ? reason.message : "无法读取容量分析"); setLoading(false);
    });
    return () => controller.abort();
  }, []);
  useEffect(() => {
    if (!trackingCleanup) {return;}
    const controller = new AbortController();
    let nextRefresh: ReturnType<typeof setTimeout> | undefined;
    const deadline = setTimeout(() => {
      controller.abort();
      clearTimeout(nextRefresh);
      setTrackingCleanup(false);
      setNotice("清理仍在后台执行；自动刷新已暂停，可稍后点击“刷新分析”查看最新结果。");
    }, 60_000);
    const update = async () => {
      try {
        const value = await fetchSnapshot(controller.signal);
        if (controller.signal.aborted) {return;}
        setSnapshot(value);
        const unreferenced = value.categories.find((category) => category.code === "PENDING_DELETE");
        if (value.details.cleanupCandidates.fileCount === 0 && unreferenced?.fileCount === 0) {
          setTrackingCleanup(false);
          setNotice("立即清理已完成，容量分析已更新。");
        } else {
          nextRefresh = setTimeout(() => void update(), 2000);
        }
      } catch (reason) {
        if (controller.signal.aborted) {return;}
        setError(reason instanceof Error ? `清理已提交，但自动刷新失败：${reason.message}` : "清理已提交，但自动刷新失败");
        setTrackingCleanup(false);
      }
    };
    void update();
    return () => {
      controller.abort();
      clearTimeout(nextRefresh);
      clearTimeout(deadline);
    };
  }, [trackingCleanup]);
  const refresh = async () => {
    setRefreshing(true); setError("");
    try {setSnapshot(await fetchSnapshot());}
    catch (reason) {setError(reason instanceof Error ? reason.message : "刷新容量分析失败");}
    finally {setRefreshing(false);}
  };
  const cleanup = async () => {
    setCleaning(true); setError(""); setNotice("");
    try {
      const result = await scheduleCleanup();
      setCleanupOpen(false);
      setNotice(result.scheduledFileCount
        ? `已安排立即清理 ${result.scheduledFileCount} 个文件（${formatStorageBytes(result.scheduledBytes)}）；正在自动更新容量分析。`
        : "当前没有仍可立即清理的未引用数据。");
      if (result.scheduledFileCount > 0) {setTrackingCleanup(true);}
      else {
        try {setSnapshot(await fetchSnapshot());}
        catch (reason) {setError(reason instanceof Error ? `清理已提交，但刷新分析失败：${reason.message}` : "清理已提交，但刷新分析失败");}
      }
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "无法开始立即清理");
    } finally {
      setCleaning(false);
    }
  };
  return {
    cleanup, cleanupOpen, cleaning: cleaning || trackingCleanup, error, loading, notice, refresh, refreshing, setCleanupOpen, snapshot,
  };
}

type StorageViewState = ReturnType<typeof useStorageAnalysis>;

function StorageHeader({ state }: { state: StorageViewState }) {
  const busy = state.loading || state.refreshing || state.cleaning;
  const canCleanup = Boolean(state.snapshot && state.snapshot.totals.pendingDeleteBytes !== "0");
  return <PageHeader
    title="容量分析"
    description="查看 Retrom 已登记文件的业务用途，定位长期数据、流程数据与等待回收内容。"
    actions={<div className="storage-header-actions">
      <button className="button danger" type="button" disabled={busy || !canCleanup} onClick={() => state.setCleanupOpen(true)}>立即清理</button>
      <button className="button secondary" type="button" disabled={busy} onClick={() => void state.refresh()}>{state.refreshing ? "正在刷新…" : "刷新分析"}</button>
    </div>}
  />;
}

function StorageFeedback({ state, generated }: { state: StorageViewState; generated: string }) {
  return <>
    {state.notice ? <FeedbackBanner tone="good">{state.notice}</FeedbackBanner> : null}
    {state.error ? <FeedbackBanner tone="bad">{state.snapshot ? `操作失败，继续显示 ${generated} 的快照：${state.error}` : state.error}</FeedbackBanner> : null}
  </>;
}

function StorageContent({ state, generated }: { state: StorageViewState; generated: string }) {
  if (state.loading) {return <LoadingState />;}
  if (!state.snapshot) {
    return <EmptyState title="容量分析暂时不可用" description="读取失败后可以重新尝试；该操作不会修改任何数据。" action={<button className="button" type="button" onClick={() => void state.refresh()}>重新读取</button>} />;
  }
  return <>
    <p className="storage-generated" aria-live="polite">统计生成于 {generated}</p>
    <Summary snapshot={state.snapshot} />
    <Breakdown snapshot={state.snapshot} />
    <Details snapshot={state.snapshot} />
  </>;
}

function StorageCleanupDialog({ state }: { state: StorageViewState }) {
  const unreferenced = state.snapshot?.categories.find((category) => category.code === "PENDING_DELETE");
  return <ConfirmDialog
    open={state.cleanupOpen}
    title="立即删除待清理文件？"
    description="这会提交待删除文件并重试失败任务。仍在使用的游戏、媒体和存档会保留。"
    confirmLabel="立即清理"
    tone="danger"
    busy={state.cleaning}
    onCancel={() => state.setCleanupOpen(false)}
    onConfirm={() => void state.cleanup()}
  >
    <p>当前快照中有 <strong>{unreferenced?.fileCount ?? 0} 个文件</strong>、<strong>{formatStorageBytes(state.snapshot?.totals.pendingDeleteBytes ?? "0")}</strong> 等待删除。实际回收量以后台复核结果为准。</p>
  </ConfirmDialog>;
}

export function StorageAnalysis() {
  const state = useStorageAnalysis();
  const { snapshot } = state;
  const generated = snapshot ? new Date(snapshot.generatedAtMs).toLocaleString("zh-CN", { hour12: false }) : "尚未生成";
  return <div className="page-layout page-layout-admin storage-analysis-page">
    <StorageHeader state={state} />
    <StorageFeedback state={state} generated={generated} />
    <StorageContent state={state} generated={generated} />
    <ScopeNote excluded={snapshot?.excluded} />
    <StorageCleanupDialog state={state} />
  </div>;
}
