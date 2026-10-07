"use client";
import { useState } from "react";
import Link from "next/link";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import styles from "./scan.module.css";
import { api, result, ApiError } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { loadDirectories, loadTags } from "@/features/library/api";
import { PageHeader, FeedbackBanner } from "@/components/ui";
import { SourcePicker } from "./source-picker";
import { ScanProgressList } from "./scan-progress";
import { useToast } from "@/components/toast-provider";
export function GameScan() {
  const { notify } = useToast();
  const [selecting, setSelecting] = useState(false);
  const [rootId, setRootId] = useState("");
  const [relativePath, setPath] = useState("");
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
  function changeSource(root: string, path: string) {
    setRootId(root);
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
          body: { rootId, relativePath, format },
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
          body: { rootId, relativePath, format, mappings },
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
        description="从服务器目录读取 Pegasus 或 EmulationStation 游戏清单，复制内容后进入统一待审核。"
        actions={
          <Link className="button secondary" href="/admin/bios">
            BIOS 文件
          </Link>
        }
      />
      <ScanSources
        onSelect={(value) => {
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
              disabled={!rootId || busy}
              onClick={() => void inspect()}
            >
              {busy ? "正在读取…" : "读取来源集合"}
            </button>
          </>
        }
      >
        <SourcePicker
          rootId={rootId}
          relativePath={relativePath}
          onChange={changeSource}
        />
        {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
      </ResponsiveSheet>
      {entries.length ? (
        <section className={`panel ${styles.selection}`}>
          <header className="panel-head">
            <div>
              <h2>核对来源集合与游戏目录</h2>
              <p>
                {entries.length} 个来源集合 · {relativePath || "来源根目录"}
              </p>
            </div>
          </header>
          {entries.map((entry, index) => (
            <div className={styles.mapping} key={entry.key}>
              <div>
                <h3>{entry.name}</h3>
                <p>
                  {entry.gameCount} 款游戏 · {entry.relativePath}
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
      <ScanProgressList />
    </>
  );
}

function ScanSources({
  onSelect,
}: {
  onSelect: (format: "pegasus" | "emulationstation") => void;
}) {
  return (
    <div className={styles.entryGrid}>
      {(["pegasus", "emulationstation"] as const).map((value) => (
        <article className={styles.entry} key={value}>
          <span>游戏目录</span>
          <h2>
            {value === "pegasus" ? "Pegasus 来源" : "EmulationStation 来源"}
          </h2>
          <p>
            读取 {value === "pegasus" ? "metadata.pegasus.txt" : "gamelist.xml"}{" "}
            游戏清单，核对集合与目标目录后复制内容。导入的游戏会进入待审核，不会自动发布。
          </p>
          <footer>
            <button className="button" onClick={() => onSelect(value)}>
              选择 {value === "pegasus" ? "Pegasus" : "EmulationStation"} 目录
            </button>
          </footer>
        </article>
      ))}
    </div>
  );
}
