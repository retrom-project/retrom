"use client";
import { useState } from "react";
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
import { useToast } from "@/components/toast-provider";
export function GameScan() {
  const { notify } = useToast();
  const [selecting, setSelecting] = useState(false);
  const [scanningBios, setScanningBios] = useState(false);
  const [path, setPath] = useState("/");
  const [sourcePending, setSourcePending] = useState(false);
  const [format, setFormat] = useState<"pegasus" | "emulationstation">(
    "pegasus",
  );
  const [entries, setEntries] = useState<Schema<"SourceEntry">[]>([]);
  const [mappings, setMappings] = useState<Schema<"SourceMapping">[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const directories = useResource(loadDirectories);
  const tags = useResource(loadTags);
  function reportFailure(failure: unknown, fallback: string) {
    const message = failure instanceof Error ? failure.message : fallback;
    if (failure instanceof ApiError && failure.status === 400) {
      setError(message);
    } else {
      notify({ tone: "bad", message });
    }
  }
  function changeSource(path: string) {
    setPath(path);
    setEntries([]);
    setMappings([]);
  }
  async function inspect() {
    setBusy(true);
    setError("");
    try {
      const data = result(
        await api.POST("/api/v1/admin/game-scans/inspect", {
          body: { path, format },
        }),
      );
      setEntries(data.items);
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
      setMappings(
        data.items.map((entry) => ({
          sourceKey: entry.key,
          platformInstanceId: "",
          tagIds: [],
        })),
      );
    } catch (failure) {
      reportFailure(failure, "无法读取来源。");
    } finally {
      setBusy(false);
    }
  }
  async function start() {
    setBusy(true);
    setError("");
    try {
      result(
        await api.POST("/api/v1/admin/game-scans", {
          body: { path, format, mappings },
        }),
      );
      notify({
        tone: "good",
        message: "扫描已开始。接收完成的游戏将进入统一待审核列表。",
      });
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
        description="从服务器目录扫描游戏来源或补齐 BIOS，统一查看扫描进度。"
      />
      <ScanSources
        onBios={() => setScanningBios(true)}
        onSelect={(value) => {
          setSourcePending(false);
          setFormat(value);
          setEntries([]);
          setMappings([]);
          setError("");
          setSelecting(true);
        }}
      />
      <ResponsiveSheet
        open={selecting}
        busy={busy}
        title="选择游戏所在目录"
        description={
          format === "pegasus"
            ? "选择包含 metadata.pegasus.txt 的来源目录。"
            : "选择包含 gamelist.xml 的来源目录。"
        }
        placement="right"
        className={styles.sheet}
        onClose={() => setSelecting(false)}
        footer={
          <>
            <button
              className="button secondary"
              disabled={busy}
              onClick={() => setSelecting(false)}
            >
              取消
            </button>
            <button
              className="button"
              disabled={sourcePending || !path.startsWith("/") || busy}
              onClick={() => void inspect()}
            >
              {busy ? "正在读取…" : "读取来源集合"}
            </button>
          </>
        }
      >
        <SourcePicker
          path={path}
          onChange={changeSource}
          onPendingChange={setSourcePending}
        />
        {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
      </ResponsiveSheet>
      {entries.length ? (
        <section className={`panel ${styles.selection}`}>
          <header className="panel-head">
            <div>
              <h2>核对来源集合与游戏目录</h2>
              <p>
                {entries.length} 个来源集合 · {path}
              </p>
            </div>
          </header>
          {entries.map((entry, index) => (
            <div className={styles.mapping} key={entry.key}>
              <div>
                <h3>{entry.name}</h3>
                <p>
                  {entry.gameCount} 款游戏 · {entry.path}
                </p>
              </div>
              <div className={styles.mappingControls}>
                <label className="field">
                  <span className="field-label">游戏目录</span>
                  <select
                    aria-label={`${entry.name}游戏目录`}
                    value={mappings[index]?.platformInstanceId ?? ""}
                    onChange={(event) =>
                      setMappings((value) =>
                        value.map((mapping, item) =>
                          item === index
                            ? {
                                ...mapping,
                                platformInstanceId: event.target.value,
                              }
                            : mapping,
                        ),
                      )
                    }
                  >
                    <option value="">选择游戏目录</option>
                    {directories.data?.items
                      .filter((directory) => directory.enabled)
                      .map((directory) => (
                        <option value={directory.id} key={directory.id}>
                          {directory.name}
                        </option>
                      ))}
                  </select>
                </label>
                <div className={styles.tagChoices}>
                  {tags.data?.items.map((tag) => (
                    <label key={tag.id}>
                      <input
                        type="checkbox"
                        checked={
                          mappings[index]?.tagIds.includes(tag.id) ?? false
                        }
                        onChange={(event) =>
                          setMappings((value) =>
                            value.map((mapping, item) =>
                              item === index
                                ? {
                                    ...mapping,
                                    tagIds: event.target.checked
                                      ? [...mapping.tagIds, tag.id]
                                      : mapping.tagIds.filter(
                                          (id) => id !== tag.id,
                                        ),
                                  }
                                : mapping,
                            ),
                          )
                        }
                      />
                      {tag.name}
                    </label>
                  ))}
                </div>
              </div>
            </div>
          ))}
          <footer className={styles.mappingFooter}>
            <button
              className="button"
              disabled={
                busy || mappings.some((mapping) => !mapping.platformInstanceId)
              }
              onClick={() => void start()}
            >
              开始扫描
            </button>
          </footer>
        </section>
      ) : null}
      {!selecting && error ? (
        <FeedbackBanner tone="bad">{error}</FeedbackBanner>
      ) : null}
      {directories.error || tags.error ? (
        <FeedbackBanner tone="bad">
          {directories.error || tags.error}
        </FeedbackBanner>
      ) : null}
      {scanningBios ? <BiosScan onClose={() => setScanningBios(false)} /> : null}
      <ScanProgressList />
    </>
  );
}

function ScanSources({ onSelect, onBios }: {
  onSelect: (format: "pegasus" | "emulationstation") => void;
  onBios: () => void;
}) {
  return <div className={styles.entryGrid}>
    <article className={styles.entry}>
      <h2>游戏扫描</h2>
      <p>读取 Pegasus 或 EmulationStation 游戏清单，核对集合与目标目录后复制内容。游戏进入待审核，不会自动发布。</p>
      <footer>{(["pegasus", "emulationstation"] as const).map((value) => <button className="button" key={value} onClick={() => onSelect(value)}>
        选择 {value === "pegasus" ? "Pegasus" : "EmulationStation"} 目录
      </button>)}</footer>
    </article>
    <article className={styles.entry}>
      <h2>BIOS扫描</h2>
      <p>按平台或核心范围补齐明确匹配的缺失 BIOS，已安装项保持原样。扫描结果可在运行依赖中查看。</p>
      <footer><button className="button" onClick={onBios}>选择 BIOS 目录</button></footer>
    </article>
  </div>;
}
