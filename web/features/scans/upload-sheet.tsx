"use client";
import { useEffect, useRef, useState, type DragEvent } from "react";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import { FeedbackBanner } from "@/components/ui";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { formatBytes } from "@/lib/format-bytes";
import styles from "./scan.module.css";
import { droppedEntries, inputEntries, mergeEntries } from "./upload-files";
import {
  cancelUpload, forgetUpload, openUpload, transferUpload, uploadProblem,
  type UploadEntry, type UploadTotals,
} from "./upload-transfer";

export type SourceFormat = Schema<"GameScanRequest">["format"];
export type UploadedSource = { uploadId: string; format: SourceFormat; fileCount: number; items: Schema<"SourceEntry">[] };
type Phase = "select" | "uploading" | "uploaded";

export const formatDescriptions: Record<SourceFormat, string> = {
  pegasus: "上传包含 metadata.pegasus.txt 的集合文件夹，元数据引用的游戏文件与封面一并上传。",
  emulationstation: "上传包含 gamelist.xml 的集合文件夹，元数据引用的游戏文件与封面一并上传。",
  files: "每个顶层文件或文件夹算一款游戏，标题取文件名；同名的 png/jpg/webp 图片作为封面。多文件游戏请放在一个文件夹中，或打包为 ZIP/7z。",
};

export function UploadSheet({ onClose, onUploaded }: {
  onClose: () => void;
  onUploaded: (source: UploadedSource) => void;
}) {
  const [format, setFormat] = useState<SourceFormat | "">("");
  const [entries, setEntries] = useState<UploadEntry[]>([]);
  const [phase, setPhase] = useState<Phase>("select");
  const [uploadId, setUploadId] = useState("");
  const [totals, setTotals] = useState<UploadTotals | null>(null);
  const [error, setError] = useState("");
  const transfer = useRef<AbortController | null>(null);
  const problem = uploadProblem(entries);
  const uploading = phase === "uploading";
  useEffect(() => {
    if (!uploading) { return; }
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [uploading]);
  useEffect(() => () => transfer.current?.abort(), []);

  function replaceSelection(next: (value: UploadEntry[]) => UploadEntry[]) {
    // A changed selection no longer matches the declared session; release its slot.
    if (uploadId) {
      void cancelUpload(uploadId).catch(() => undefined);
      setUploadId("");
      setTotals(null);
    }
    setEntries(next);
    setError("");
  }
  function add(added: UploadEntry[]) {
    replaceSelection((value) => mergeEntries(value, added));
  }
  async function inspect(id: string, selected: SourceFormat) {
    const data = result(await api.POST("/api/v1/admin/game-scans/inspect", { body: { uploadId: id, format: selected } }));
    forgetUpload();
    onUploaded({ uploadId: id, format: selected, fileCount: entries.length, items: data.items });
  }
  async function start() {
    if (!format || problem) { return; }
    const controller = new AbortController();
    transfer.current = controller;
    setPhase("uploading");
    setError("");
    let transferred = false;
    try {
      const session = await openUpload(entries);
      setUploadId(session.id);
      await transferUpload(session, entries, setTotals, controller.signal);
      transferred = true;
      setPhase("uploaded");
      await inspect(session.id, format);
    } catch (failure) {
      if (controller.signal.aborted) { return; }
      setPhase(transferred ? "uploaded" : "select");
      setError(failure instanceof Error ? failure.message : "上传失败，请重试。");
    }
  }
  async function retryInspect() {
    if (!format) { return; }
    setError("");
    try {
      await inspect(uploadId, format);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "无法读取来源集合。");
    }
  }
  async function abandon() {
    transfer.current?.abort();
    if (uploadId) {
      try { await cancelUpload(uploadId); } catch { /* an expired session is already gone */ }
    }
    onClose();
  }

  const totalBytes = entries.reduce((sum, entry) => sum + entry.file.size, 0);
  return (
    <ResponsiveSheet
      open
      busy={uploading}
      title="浏览器上传游戏"
      description="上传完成后按所选形式读取来源集合，再映射游戏目录开始扫描。"
      placement="right"
      className={styles.sheet}
      onClose={uploading ? () => undefined : onClose}
      footer={<UploadFooter phase={phase} started={!!uploadId} ready={!!format && !problem}
        onCancel={() => void (uploadId || uploading ? abandon() : onClose())}
        onStart={() => void start()} onInspect={() => void retryInspect()} />}
    >
      <div className={styles.picker}>
        <label className="field">
          <span className="field-label">文件组织形式</span>
          <select aria-label="上传文件组织形式" value={format} disabled={uploading}
            onChange={(event) => { setFormat(event.target.value as SourceFormat | ""); setError(""); }}>
            <option value="">请选择文件组织形式</option>
            <option value="pegasus">metadata.pegasus.txt</option>
            <option value="emulationstation">gamelist.xml</option>
            <option value="files">散装文件（每个文件或文件夹一款游戏）</option>
          </select>
          {format ? <small>{formatDescriptions[format]}</small> : null}
        </label>
        <UploadDropZone enabled={phase === "select"} onAdd={add} />
        {entries.length ? <UploadSummary entries={entries} totalBytes={totalBytes} locked={phase !== "select"}
          onClear={() => replaceSelection(() => [])} /> : null}
        {totals && phase !== "select" ? <UploadProgress totals={totals} /> : null}
        {entries.length && problem ? <FeedbackBanner tone="bad">{problem}</FeedbackBanner> : null}
        {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
      </div>
    </ResponsiveSheet>
  );
}

function UploadSummary({ entries, totalBytes, locked, onClear }: {
  entries: UploadEntry[]; totalBytes: number; locked: boolean; onClear: () => void;
}) {
  const shown = entries.slice(0, 8);
  return <section className={styles.uploadSummary} aria-label="已选择的文件">
    <header>
      <strong>{entries.length} 个文件 · {formatBytes(totalBytes)}</strong>
      {locked ? null : <button type="button" className="button secondary" onClick={onClear}>清空</button>}
    </header>
    <ul>
      {shown.map((entry) => <li key={entry.path}><span>{entry.path}</span><small>{formatBytes(entry.file.size)}</small></li>)}
    </ul>
    {entries.length > shown.length ? <small>以及另外 {entries.length - shown.length} 个文件</small> : null}
  </section>;
}

function UploadProgress({ totals }: { totals: UploadTotals }) {
  return <section className={styles.reviewProgress} aria-label="上传进度">
    <p>已上传 {formatBytes(totals.receivedBytes)} / {formatBytes(totals.totalBytes)} · 文件 {totals.completedFiles} / {totals.totalFiles}</p>
    <progress aria-label="上传进度" value={totals.receivedBytes} max={Math.max(totals.totalBytes, 1)} />
  </section>;
}

function UploadFooter({ phase, started, ready, onCancel, onStart, onInspect }: {
  phase: Phase; started: boolean; ready: boolean; onCancel: () => void; onStart: () => void; onInspect: () => void;
}) {
  const uploading = phase === "uploading";
  return <>
    <button className="button secondary" onClick={onCancel}>{started || uploading ? "取消上传" : "关闭"}</button>
    {phase === "uploaded"
      ? <button className="button" disabled={!ready} onClick={onInspect}>读取来源集合</button>
      : <button className="button" disabled={!ready || uploading} onClick={onStart}>
        {uploading ? "正在上传…" : started ? "继续上传" : "开始上传"}
      </button>}
  </>;
}

function UploadDropZone({ enabled, onAdd }: { enabled: boolean; onAdd: (entries: UploadEntry[]) => void }) {
  const [dragging, setDragging] = useState(false);
  async function drop(event: DragEvent) {
    event.preventDefault();
    setDragging(false);
    if (enabled) { onAdd(await droppedEntries(event.dataTransfer)); }
  }
  return <div
    className={`${styles.dropZone} ${dragging ? styles.dropActive : ""}`}
    onDragOver={(event) => { event.preventDefault(); setDragging(enabled); }}
    onDragLeave={() => setDragging(false)}
    onDrop={(event) => void drop(event)}
  >
    <p>将文件或文件夹拖到这里，或</p>
    <div className={styles.dropActions}>
      <label className="button secondary">
        选择文件
        <input type="file" multiple hidden disabled={!enabled}
          onChange={(event) => { onAdd(inputEntries(event.target.files)); event.target.value = ""; }} />
      </label>
      <label className="button secondary">
        选择文件夹
        <input type="file" hidden disabled={!enabled} ref={(node) => node?.setAttribute("webkitdirectory", "")}
          onChange={(event) => { onAdd(inputEntries(event.target.files)); event.target.value = ""; }} />
      </label>
    </div>
    <small>单文件不超过 4 GiB，单次合计不超过 20 GiB。</small>
  </div>;
}
