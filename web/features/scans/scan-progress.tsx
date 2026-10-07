"use client";
import Link from "next/link";
import { useEffect } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { ResourceState } from "@/components/resource-state";
import { BrowserTime } from "@/components/browser-time";
import styles from "./scan.module.css";
import { useToast } from "@/components/toast-provider";
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
  const { notify } = useToast();
  const scans = useResource(load);
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
      notify({ tone: "good", message: "扫描已取消。" });
    } catch (failure) {
      notify({
        tone: "bad",
        message: failure instanceof Error ? failure.message : "取消失败。",
      });
    }
  }
  return (
    <section className={styles.progress}>
      <header>
        <h2>当前扫描进度</h2>
        <p>
          扫描离开页面后仍会继续。游戏扫描进入统一待审核，BIOS
          扫描查看当前安装。
        </p>
      </header>
      <ResourceState resource={scans}>
        {(data) => (
          <div className="stack">
            {data.items.map((scan) => (
              <article className={styles.progressRow} key={scan.id}>
                <Link href={scanDestination(scan.scanType)}>
                  <div>
                    <h3>
                      <strong>
                        {scan.scanType === "game"
                          ? "游戏扫描"
                          : "BIOS 扫描补齐"}
                      </strong>
                      <span
                        className={`status ${scan.status === "completed" ? "good" : scan.status === "failed" ? "bad" : "neutral"}`}
                      >
                        {labels[scan.status]}
                      </span>
                    </h3>
                    <p>
                      更新时间 · <BrowserTime value={scan.updatedAtMs} />
                    </p>
                    {scan.error ? <p role="alert">{scan.error}</p> : null}
                  </div>
                  <div className={styles.counts}>
                    <div>
                      <small>
                        {scan.scanType === "game" ? "总游戏数" : "总要求数"}
                      </small>
                      <strong>
                        {scan.totalKnown ? scan.totalCount : "发现中"}
                      </strong>
                    </div>
                    <div>
                      <small>
                        {scan.scanType === "game" ? "已扫描" : "已处理"}
                      </small>
                      <strong>{scan.processedCount}</strong>
                    </div>
                    <div>
                      <small>
                        {scan.scanType === "game" ? "已导入" : "已补齐"}
                      </small>
                      <strong>{scan.importedCount}</strong>
                    </div>
                    <div>
                      <small>已跳过</small>
                      <strong>{scan.skippedCount}</strong>
                    </div>
                    <div>
                      <small>失败</small>
                      <strong>{scan.failedCount}</strong>
                    </div>
                  </div>
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
              <p className="panel compact-empty">暂无扫描任务。</p>
            ) : null}
          </div>
        )}
      </ResourceState>
    </section>
  );
}
