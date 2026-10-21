"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import styles from "./scan.module.css";
import { api, result, ApiError } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { loadDirectories, loadTags } from "@/features/library/api";
import { PageHeader, FeedbackBanner } from "@/components/ui";
import { SourcePicker } from "./source-picker";
import { BiosScan } from "./bios-scan";
import { ScanProgressList } from "./scan-progress";
import { CollectionMapping } from "./collection-mapping";
import { useToast } from "@/components/toast-provider";
import { usePhoneLayout } from "@/lib/use-phone-layout";
import { formatDescriptions, type UploadedSource } from "./upload-sheet";
import { useBrowserUploads } from "./browser-uploads";
type SourceFormat = Schema<"GameScanRequest">["format"] | "";
type UploadedFiles = { uploadId: string; fileCount: number };

export function GameScan() {
  const router = useRouter();
  const { notify } = useToast();
  const [selecting, setSelecting] = useState(false);
  const phone = usePhoneLayout();
  const [upload, setUpload] = useState<UploadedFiles | null>(null);
  const [scanningBios, setScanningBios] = useState(false);
  const [scanRevision, setScanRevision] = useState(0);
  const [path, setPath] = useState("/");
  const [format, setFormat] = useState<SourceFormat>("");
  const [entries, setEntries] = useState<Schema<"SourceEntry">[]>([]);
  const [mappings, setMappings] = useState<Schema<"SourceMapping">[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const directories = useResource(loadDirectories);
  const tags = useResource(loadTags);
  const uploads = useBrowserUploads({ phone, onUploaded: uploaded, onFailure: reportFailure, setBusy });
  function reportFailure(failure: unknown, fallback: string) {
    const message = failure instanceof Error ? failure.message : fallback;
    if (failure instanceof ApiError && failure.status === 400) {
      setError(message);
    } else {
      notify({ tone: "bad", message });
    }
  }
  // A loose upload's only collection is the staging tree; name it for what it is.
  function collectionName(entry: Schema<"SourceEntry">) {
    return upload && format === "files" ? "上传的游戏文件" : entry.name;
  }
  function showCollections(items: Schema<"SourceEntry">[]) {
    setEntries(items);
    setMappings(items.map((entry) => ({ sourceKey: entry.key, platformInstanceId: "", tagIds: [] })));
  }
  function uploaded(source: UploadedSource) {
    setFormat(source.format);
    setUpload({ uploadId: source.uploadId, fileCount: source.fileCount });
    setError("");
    showCollections(source.items);
    notify({ tone: "good", message: `上传完成，已读取 ${source.items.length} 个来源集合。` });
  }
  function changeSource(path: string) {
    setPath(path);
    setEntries([]);
    setMappings([]);
  }
  async function inspect() {
    if (!format) {
      return;
    }
    setBusy(true);
    setError("");
    try {
      const data = result(
        await api.POST("/api/v1/admin/game-scans/inspect", {
          body: { path, format },
        }),
      );
      setUpload(null);
      showCollections(data.items);
      notify(
        data.items.length
          ? {
              tone: "good",
              message: `已读取 ${data.items.length} 个来源集合。`,
            }
          : {
              tone: "warn",
              message: "所选目录中未发现来源集合，请选择其他目录。",
            },
      );
      setSelecting(false);
    } catch (failure) {
      reportFailure(failure, "无法读取来源。");
    } finally {
      setBusy(false);
    }
  }
  async function start() {
    if (!format) {
      return;
    }
    setBusy(true);
    setError("");
    try {
      const scan = result(
        await api.POST("/api/v1/admin/game-scans", {
          body: upload ? { uploadId: upload.uploadId, format, mappings } : { path, format, mappings },
        }),
      );
      notify({
        tone: "good",
        message: "扫描已开始。接收完成的游戏将进入统一待审核列表。",
      });
      uploads.reload();
      router.push(`/admin/reviews?scanId=${encodeURIComponent(scan.id)}`);
    } catch (failure) {
      reportFailure(failure, "扫描启动失败。");
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHeader
        title="来源扫描"
        description="从服务器目录或浏览器上传扫描游戏来源，或补齐 BIOS，统一查看扫描进度。"
      />
      <ScanSources
        onBios={() => setScanningBios(true)}
        phone={phone}
        onUpload={() => { setError(""); uploads.open(); }}
        onSelect={() => {
          setFormat("");
          setEntries([]);
          setMappings([]);
          setError("");
          setSelecting(true);
        }}
      />
      {selecting ? <GameSourcePicker
        busy={busy}
        path={path}
        format={format}
        error={error}
        onPathChange={changeSource}
        onFormatChange={(value) => { setFormat(value); setError(""); }}
        onClose={() => setSelecting(false)}
        onInspect={() => void inspect()}
      /> : null}
      {uploads.view}
      {entries.length ? (
        <CollectionMapping
          entries={entries}
          mappings={mappings}
          onMappingsChange={setMappings}
          directories={directories.data?.items}
          tags={tags.data?.items}
          summary={upload ? `浏览器上传 · ${upload.fileCount} 个文件` : path}
          showPaths={!upload}
          nameOf={collectionName}
          disabled={!format || busy || mappings.some((mapping) => !mapping.platformInstanceId)}
          onStart={() => void start()}
        />
      ) : null}
      {!selecting && error ? (
        <FeedbackBanner tone="bad">{error}</FeedbackBanner>
      ) : null}
      {directories.error || tags.error ? (
        <FeedbackBanner tone="bad">
          {directories.error || tags.error}
        </FeedbackBanner>
      ) : null}
      {scanningBios ? <BiosScan onClose={() => setScanningBios(false)} onStarted={() => setScanRevision((value) => value + 1)} /> : null}
      <ScanProgressList revision={scanRevision} />
    </>
  );
}

function GameSourcePicker({ busy, path, format, error, onPathChange, onFormatChange, onClose, onInspect }: {
  busy: boolean;
  path: string;
  format: SourceFormat;
  error: string;
  onPathChange: (path: string) => void;
  onFormatChange: (format: SourceFormat) => void;
  onClose: () => void;
  onInspect: () => void;
}) {
  const [sourcePending, setSourcePending] = useState(false);
  return (
    <ResponsiveSheet
      open
      busy={busy}
      title="选择游戏所在目录"
      description={
        format === "pegasus"
          ? "选择包含 metadata.pegasus.txt 的来源目录。"
          : format === "emulationstation"
            ? "选择包含 gamelist.xml 的来源目录。"
            : format === "files"
              ? `选择存放游戏的目录。${formatDescriptions.files}`
              : "选择服务器目录，并指定文件组织形式。"
      }
      placement="right"
      className={styles.sheet}
      onClose={onClose}
      footer={
        <>
          <button
            className="button secondary"
            disabled={busy}
            onClick={onClose}
          >
            取消
          </button>
          <button
            className="button"
            disabled={!format || sourcePending || !path.startsWith("/") || busy}
            onClick={onInspect}
          >
            {busy ? "正在读取…" : "读取来源集合"}
          </button>
        </>
      }
    >
      <SourcePicker
        path={path}
        onChange={onPathChange}
        onPendingChange={setSourcePending}
      >
        <label className="field">
          <span className="field-label">文件组织形式</span>
          <select
            aria-label="文件组织形式"
            value={format}
            required
            disabled={busy}
            onChange={(event) => onFormatChange(event.target.value as SourceFormat)}
          >
            <option value="">请选择文件组织形式</option>
            <option value="pegasus">metadata.pegasus.txt</option>
            <option value="emulationstation">gamelist.xml</option>
            <option value="files">散装文件（每个文件或文件夹一款游戏）</option>
          </select>
        </label>
      </SourcePicker>
      {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
    </ResponsiveSheet>
  );
}

// Uploading gigabytes of games is a desktop task; phones keep server scans only.
function ScanSources({ phone, onSelect, onBios, onUpload }: {
  phone: boolean;
  onSelect: () => void;
  onBios: () => void;
  onUpload: () => void;
}) {
  return <div className={styles.entryGrid}>
    <article className={styles.entry}>
      <h2>游戏扫描</h2>
      <p>读取 Pegasus、EmulationStation 游戏清单或散装游戏文件，核对集合与目标目录后复制内容。游戏进入待审核，不会自动发布。</p>
      <footer>
        {phone ? null : <button className="button secondary" onClick={onUpload}>浏览器上传</button>}
        <button className="button" onClick={onSelect}>本地扫描</button>
      </footer>
    </article>
    <article className={styles.entry}>
      <h2>BIOS扫描</h2>
      <p>按平台或核心范围补齐明确匹配的缺失 BIOS，已安装项保持原样。扫描结果可在运行依赖中查看。</p>
      <footer><button className="button" onClick={onBios}>选择 BIOS 目录</button></footer>
    </article>
  </div>;
}
