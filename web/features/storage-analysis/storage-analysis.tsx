"use client";

import { useEffect, useState } from "react";
import { EmptyState, FeedbackBanner, PageHeader } from "@/components/ui";
import { responseError } from "@/lib/upload";
import { formatStorageBytes, storageBarWidth, storagePercentage } from "./format";
import {
  categoryPresentation,
  excludedPresentation,
  type StorageCategory,
  type StorageSnapshot,
} from "./model";

const storageEndpoint = "/api/v1/admin/storage-analysis";
const fixedExcluded = Object.keys(excludedPresentation) as StorageSnapshot["excluded"];

async function fetchSnapshot(signal?: AbortSignal): Promise<StorageSnapshot> {
  const response = await fetch(storageEndpoint, { cache: "no-store", credentials: "same-origin", signal });
  if (!response.ok) {throw new Error(await responseError(response, "无法读取容量分析"));}
  return response.json() as Promise<StorageSnapshot>;
}

function ByteValue({ bytes, label, className }: { bytes: string; label: string; className?: string }) {
  return <span className={className} tabIndex={0} title={`${bytes} bytes`} aria-label={`${label}，精确值 ${bytes} bytes`}>
    {formatStorageBytes(bytes)}
  </span>;
}

function Summary({ snapshot }: { snapshot: StorageSnapshot }) {
  const items = [
    { label: "已登记 CAS", bytes: snapshot.totals.registeredBytes, note: `${snapshot.totals.blobCount} 个 Blob` },
    { label: "受保护数据", bytes: snapshot.totals.protectedBytes, note: `${storagePercentage(snapshot.totals.protectedBytes, snapshot.totals.registeredBytes)} 已被引用` },
    { label: "等待回收", bytes: snapshot.totals.unreferencedBytes, note: `${storagePercentage(snapshot.totals.unreferencedBytes, snapshot.totals.registeredBytes)} 当前未引用` },
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
  return <div className="storage-bar" role="img" aria-label={`已登记容量 ${formatStorageBytes(snapshot.totals.registeredBytes)}，按九个用途分类`}>
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
      <small>{category.blobCount} 个 Blob · {storagePercentage(category.bytes, total)}</small>
    </div>
  </article>;
}

function Breakdown({ snapshot }: { snapshot: StorageSnapshot }) {
  if (snapshot.totals.registeredBytes === "0") {
    return <EmptyState title="还没有已登记的 CAS 数据" description="导入游戏、安装 BIOS 或创建存档后，这里会按实际 Blob 引用显示容量。" />;
  }
  return <section className="panel storage-breakdown" aria-labelledby="storage-breakdown-title">
    <div className="panel-head"><div><h2 id="storage-breakdown-title">按用途分析</h2><p>同一个 Blob 只统计一次；长期业务用途优先于流程和运行快照。</p></div></div>
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
  return <section className="storage-details" aria-label="引用视图">
    <article className="panel">
      <div><span>存档引用视图</span><strong>{saves.activeCount} 份有效 · {saves.deletedCount} 份软删除</strong></div>
      <dl>
        <div><dt>状态文件</dt><dd><ByteValue bytes={saves.stateReferenceBytes} label="存档状态文件引用量" /></dd></div>
        <div><dt>截图文件</dt><dd><ByteValue bytes={saves.screenshotReferenceBytes} label="存档截图引用量" /></dd></div>
      </dl>
      <p>状态文件与截图是两个可重叠的引用视图，不与用途分类相加。</p>
    </article>
    <article className="panel">
      <div><span>清理候选视图</span><strong>{cleanup.blobCount} 个 Blob</strong></div>
      <ByteValue className="storage-detail-total" bytes={cleanup.bytes} label="清理候选引用量" />
      <p>仅显示 GC 候选登记，不代表这些数据会立即删除，也不提供清理操作。</p>
    </article>
  </section>;
}

function ScopeNote({ excluded = fixedExcluded }: { excluded?: StorageSnapshot["excluded"] }) {
  return <section className="storage-scope" aria-labelledby="storage-scope-title">
    <div><p className="eyebrow">统计边界</p><h2 id="storage-scope-title">仅计算已登记 CAS payload</h2><p>口径版本 <code>REGISTERED_CAS_PAYLOAD_V1</code>。页面不等同于磁盘占用或可用空间。</p></div>
    <ul>{excluded.map((code) => <li key={code}>{excludedPresentation[code]}</li>)}</ul>
  </section>;
}

function LoadingState() {
  return <div className="storage-loading" role="status" aria-label="正在读取容量分析">
    <i /><i /><i /><div /><div />
  </div>;
}

export function StorageAnalysis() {
  const [snapshot, setSnapshot] = useState<StorageSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState("");
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
  const refresh = async () => {
    setRefreshing(true); setError("");
    try {setSnapshot(await fetchSnapshot());}
    catch (reason) {setError(reason instanceof Error ? reason.message : "刷新容量分析失败");}
    finally {setRefreshing(false);}
  };
  const generated = snapshot ? new Date(snapshot.generatedAtMs).toLocaleString("zh-CN", { hour12: false }) : "尚未生成";
  return <div className="page-layout page-layout-admin storage-analysis-page">
    <PageHeader eyebrow="系统存储" title="容量分析" description="查看 Retrom 已登记 CAS 数据的业务用途，定位长期数据、流程数据与等待回收内容。" actions={<button className="button secondary" type="button" disabled={loading || refreshing} onClick={() => void refresh()}>{refreshing ? "正在刷新…" : "刷新分析"}</button>} />
    {error ? <FeedbackBanner tone="bad">{snapshot ? `刷新失败，继续显示 ${generated} 的快照：${error}` : error}</FeedbackBanner> : null}
    {loading ? <LoadingState /> : snapshot ? <>
      <p className="storage-generated" aria-live="polite">统计生成于 {generated}</p>
      <Summary snapshot={snapshot} />
      <Breakdown snapshot={snapshot} />
      <Details snapshot={snapshot} />
    </> : <EmptyState title="容量分析暂时不可用" description="读取失败后可以重新尝试；该操作不会修改任何数据。" action={<button className="button" type="button" onClick={() => void refresh()}>重新读取</button>} />}
    <ScopeNote excluded={snapshot?.excluded} />
  </div>;
}
