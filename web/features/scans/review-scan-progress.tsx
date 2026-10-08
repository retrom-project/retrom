"use client";
import Link from "next/link";
import { useCallback, useEffect, useRef } from "react";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import styles from "./scan.module.css";

async function loadScan(scanId: string) {
  // Bound each lookup; never substitute a newer task for the requested one.
  for (let offset = 0; offset < 1000; offset += 100) {
    const page = result(await api.GET("/api/v1/admin/scans", {
      params: { query: { limit: 100, offset } },
    }));
    const scan = page.items.find((item) => item.id === scanId);
    if (scan) { return scan.scanType === "game" ? scan : null; }
    if (page.items.length < 100) { break; }
  }
  return null;
}

export function ReviewScanProgress({ scanId, onChange }: { scanId: string; onChange: () => void }) {
  const load = useCallback(() => loadScan(scanId), [scanId]);
  const scan = useResource(load);
  const lastProgress = useRef("");
  useEffect(() => {
    if (!scan.data) { return; }
    const { id, processedCount, importedCount, status } = scan.data;
    const progress = JSON.stringify([id, processedCount, importedCount, status]);
    if (progress !== lastProgress.current) {
      lastProgress.current = progress;
      onChange();
    }
    if (status !== "pending" && status !== "running") { return; }
    const timer = setTimeout(scan.reload, 3000);
    return () => clearTimeout(timer);
  }, [scan.data, scan.reload, onChange]);
  return <section className={styles.reviewProgress} aria-label="当前游戏扫描">
    <header><h2>游戏扫描进度</h2><Link className="button secondary" href="/admin/imports/server">查看来源扫描</Link></header>
    {scan.loading ? <p role="status">正在读取当前扫描…</p> : scan.error ? <p role="alert">无法读取扫描进度：{scan.error}</p>
      : scan.data ? <ScanStatus scan={scan.data} /> : <p role="status">未找到这项扫描，任务可能已清理。可前往来源扫描查看。</p>}
    {!scan.loading && !scan.data ? <button className="button secondary" type="button" onClick={scan.reload}>重新读取进度</button> : null}
  </section>;
}

function ScanStatus({ scan }: { scan: Schema<"ScanProgress"> }) {
  const labels = { pending: "等待扫描", running: "扫描中", completed: scan.failedCount ? "扫描完成，部分游戏失败" : "扫描已完成", cancelled: "扫描已取消", failed: "扫描已中断" };
  const running = scan.status === "pending" || scan.status === "running";
  return <>
    <p role="status">{labels[scan.status]} · 已扫描 {scan.processedCount} / {scan.totalKnown ? scan.totalCount : "总数发现中"}</p>
    {running || scan.totalKnown ? <progress aria-label="游戏扫描进度" max={Math.max(1, scan.totalCount)} value={scan.totalKnown ? scan.processedCount : undefined} /> : null}
    <p>已导入 {scan.importedCount} · 已跳过 {scan.skippedCount} · 失败 {scan.failedCount}</p>
    {scan.error ? <p role="alert">{scan.error}</p> : null}
  </>;
}
