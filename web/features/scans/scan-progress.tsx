"use client";
import Link from "next/link";
import { useEffect, useState } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { ResourceState } from "@/components/resource-state";
export function scanDestination(type: "game" | "bios") {
  return type === "game" ? "/admin/reviews" : "/admin/bios";
}
async function load() {
  return result(await api.GET("/api/v1/admin/scans"));
}
const labels = {
  pending: "等待扫描",
  running: "扫描中",
  completed: "已完成",
  cancelled: "已取消",
  failed: "已中断",
};
export function ScanProgressList() {
  const scans = useResource(load);
  const [error, setError] = useState("");
  useEffect(() => {
    const timer = setInterval(scans.reload, 3000);
    return () => clearInterval(timer);
  }, [scans.reload]);
  async function cancel(scanId: string) {
    try {
      result(
        await api.POST("/api/v1/admin/scans/{scanId}/cancel", {
          params: { path: { scanId } },
        }),
      );
      scans.reload();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "取消失败。");
    }
  }
  return (
    <section className="workspace-section">
      <h2>当前扫描进度</h2>
      {error ? <p role="alert">{error}</p> : null}
      <ResourceState resource={scans}>
        {(data) => (
          <div className="stack">
            {data.items.map((scan) => (
              <article className="workspace-row" key={scan.id}>
                <Link href={scanDestination(scan.scanType)}>
                  <h3>
                    {scan.scanType === "game" ? "游戏扫描" : "BIOS 扫描补齐"} ·{" "}
                    {labels[scan.status]}
                  </h3>
                  <div className="scan-counts">
                    <span>
                      {scan.scanType === "game" ? "总游戏数" : "总要求数"}{" "}
                      {scan.totalKnown ? scan.totalCount : "发现中"}
                    </span>
                    <span>
                      {scan.scanType === "game" ? "已扫描" : "已处理"}{" "}
                      {scan.processedCount}
                    </span>
                    <span>
                      {scan.scanType === "game" ? "已导入" : "已补齐"}{" "}
                      {scan.importedCount}
                    </span>
                    <span>已跳过 {scan.skippedCount}</span>
                    <span>失败 {scan.failedCount}</span>
                  </div>
                  {scan.error ? <p role="alert">{scan.error}</p> : null}
                </Link>
                {scan.status === "pending" || scan.status === "running" ? (
                  <button
                    className="button secondary"
                    onClick={() => void cancel(scan.id)}
                  >
                    取消
                  </button>
                ) : null}
              </article>
            ))}
            {!data.items.length ? (
              <p className="workspace-note">暂无扫描任务。</p>
            ) : null}
          </div>
        )}
      </ResourceState>
    </section>
  );
}
