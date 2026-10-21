"use client";
import { useState } from "react";
import { BrowserTime } from "@/components/browser-time";
import { useToast } from "@/components/toast-provider";
import { api, result } from "@/lib/api/client";
import { formatBytes } from "@/lib/format-bytes";
import type { Resource } from "@/lib/use-resource";
import styles from "./scan.module.css";
import { cancelUpload, type UploadSession } from "./upload-transfer";

export const formatLabels: Record<UploadSession["format"], string> = {
  pegasus: "metadata.pegasus.txt",
  emulationstation: "gamelist.xml",
  files: "散装文件",
};

export async function loadUploads() {
  return result(await api.GET("/api/v1/admin/uploads"));
}

/**
 * Uploads of this administrator that have not started a scan. They outlive a reload, so a
 * finished upload can still be mapped and an interrupted one resumed or released.
 */
export function PendingUploads({ uploads, canUpload, onMap, onResume }: {
  uploads: Resource<{ items: UploadSession[] }>;
  canUpload: boolean;
  onMap: (upload: UploadSession) => void;
  onResume: (upload: UploadSession) => void;
}) {
  const { notify } = useToast();
  const [busy, setBusy] = useState("");
  if (!uploads.data?.items.length) { return null; }
  async function abandon(id: string) {
    setBusy(id);
    try {
      await cancelUpload(id);
      notify({ tone: "good", message: "已放弃上传并删除暂存文件。" });
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "放弃上传失败。" });
    } finally {
      setBusy("");
      uploads.reload();
    }
  }
  return (
    <section className={styles.progress} aria-label="未完成的上传">
      <header>
        <div>
          <h2>未完成的上传</h2>
          <p>已上传的内容在最后一次上传 24 小时后失效；开始扫描后会从这里移除。</p>
        </div>
      </header>
      <div className="stack">
            {uploads.data.items.map((upload) => {
              const total = upload.files.reduce((sum, file) => sum + file.sizeBytes, 0);
              const received = upload.files.reduce((sum, file) => sum + file.receivedBytes, 0);
              return (
                <article className={`${styles.progressRow} ${styles.uploadRow}`} key={upload.id}>
                  <div>
                    <h3>
                      <strong>浏览器上传</strong>
                      <span className={`status ${upload.completed ? "good" : "neutral"}`}>
                        {upload.completed ? "待映射目录" : "未传完"}
                      </span>
                    </h3>
                    <p>
                      {upload.files.length} 个文件 · {formatLabels[upload.format]} · 已上传 {formatBytes(received)} / {formatBytes(total)}
                    </p>
                    <p>有效至 <BrowserTime value={upload.expiresAtMs} /></p>
                  </div>
                  <div className={styles.uploadActions}>
                    <button className="button secondary" disabled={busy === upload.id} onClick={() => void abandon(upload.id)}>
                      放弃
                    </button>
                    {upload.completed
                      ? <button className="button" disabled={!!busy} onClick={() => onMap(upload)}>继续映射</button>
                      : canUpload
                        ? <button className="button" disabled={!!busy} onClick={() => onResume(upload)}>继续上传</button>
                        : null}
                  </div>
                </article>
              );
            })}
      </div>
    </section>
  );
}
