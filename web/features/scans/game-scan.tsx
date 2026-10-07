"use client";
import { useState } from "react";
import { api, result } from "@/lib/api/client";
import type { Schema } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { loadDirectories, loadTags } from "@/features/library/api";
import { PageHeader, FeedbackBanner } from "@/components/ui";
import { SourcePicker } from "./source-picker";
import { ScanProgressList } from "./scan-progress";
export function GameScan() {
  const [rootId, setRootId] = useState("");
  const [relativePath, setPath] = useState("");
  const [format, setFormat] = useState<"pegasus" | "emulationstation">(
    "pegasus",
  );
  const [entries, setEntries] = useState<Schema<"SourceEntry">[]>([]);
  const [mappings, setMappings] = useState<Schema<"SourceMapping">[]>([]);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);
  const directories = useResource(loadDirectories);
  const tags = useResource(loadTags);
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
      setMappings(
        data.items.map((entry) => ({
          sourceKey: entry.key,
          platformInstanceId: "",
          tagIds: [],
        })),
      );
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "无法读取来源。");
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
      setMessage("扫描已开始。接收完成的游戏将进入统一待审核列表。");
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "扫描启动失败。");
    } finally {
      setBusy(false);
    }
  }
  return (
    <>
      <PageHeader
        title="来源扫描"
        description="扫描 Pegasus 或 EmulationStation 清单，并复制游戏内容到受管存储。"
      />
      <section className="workspace-card stack">
        <div className="workspace-actions">
          <button
            className={`button ${format === "pegasus" ? "" : "secondary"}`}
            onClick={() => {
              setFormat("pegasus");
              setEntries([]);
            }}
          >
            Pegasus
          </button>
          <button
            className={`button ${format === "emulationstation" ? "" : "secondary"}`}
            onClick={() => {
              setFormat("emulationstation");
              setEntries([]);
            }}
          >
            EmulationStation
          </button>
        </div>
        <SourcePicker
          rootId={rootId}
          relativePath={relativePath}
          onChange={changeSource}
        />
        <button
          className="button secondary"
          disabled={!rootId || busy}
          onClick={() => void inspect()}
        >
          读取来源集合
        </button>
        {entries.map((entry, index) => (
          <div className="workspace-row" key={entry.key}>
            <div>
              <h3>{entry.name}</h3>
              <p>
                {entry.gameCount} 款游戏 · {entry.relativePath}
              </p>
            </div>
            <div className="stack">
              <select
                aria-label={`${entry.name}游戏目录`}
                value={mappings[index]?.platformInstanceId ?? ""}
                onChange={(event) =>
                  setMappings((value) =>
                    value.map((mapping, item) =>
                      item === index
                        ? { ...mapping, platformInstanceId: event.target.value }
                        : mapping,
                    ),
                  )
                }
              >
                <option value="">选择游戏目录</option>
                {directories.data?.items.map((directory) => (
                  <option value={directory.id} key={directory.id}>
                    {directory.name}
                  </option>
                ))}
              </select>
              <div className="workspace-tags">
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
        {entries.length ? (
          <button
            className="button"
            disabled={
              busy || mappings.some((mapping) => !mapping.platformInstanceId)
            }
            onClick={() => void start()}
          >
            开始扫描
          </button>
        ) : null}
        {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
        {message ? (
          <FeedbackBanner tone="good">{message}</FeedbackBanner>
        ) : null}
      </section>
      <ScanProgressList />
    </>
  );
}
