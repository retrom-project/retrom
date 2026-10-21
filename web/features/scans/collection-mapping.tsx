"use client";
import type { Dispatch, SetStateAction } from "react";
import type { Schema } from "@/lib/api/types";
import styles from "./scan.module.css";

/** Maps each source collection to a game directory and tags before a scan starts. */
export function CollectionMapping({
  entries, mappings, onMappingsChange, directories = [], tags = [], summary, showPaths, nameOf, disabled, onStart,
}: {
  entries: Schema<"SourceEntry">[];
  mappings: Schema<"SourceMapping">[];
  onMappingsChange: Dispatch<SetStateAction<Schema<"SourceMapping">[]>>;
  directories?: Schema<"Directory">[];
  tags?: Schema<"Tag">[];
  summary: string;
  showPaths: boolean;
  nameOf: (entry: Schema<"SourceEntry">) => string;
  disabled: boolean;
  onStart: () => void;
}) {
  return (
    <section className={`panel ${styles.selection}`}>
      <header className="panel-head">
        <div>
          <h2>核对来源集合与游戏目录</h2>
          <p>
            {entries.length} 个来源集合 · {summary}
          </p>
        </div>
      </header>
      {entries.map((entry, index) => (
        <div className={styles.mapping} key={entry.key}>
          <div>
            <h3>{nameOf(entry)}</h3>
            <p>
              {entry.gameCount} 款游戏{showPaths ? ` · ${entry.path}` : ""}
            </p>
          </div>
          <div className={styles.mappingControls}>
            <label className="field">
              <span className="field-label">游戏目录</span>
              <select
                aria-label={`${nameOf(entry)}游戏目录`}
                value={mappings[index]?.platformInstanceId ?? ""}
                onChange={(event) =>
                  onMappingsChange((value) =>
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
                {directories
                  .filter((directory) => directory.enabled)
                  .map((directory) => (
                    <option value={directory.id} key={directory.id}>
                      {directory.name}
                    </option>
                  ))}
              </select>
            </label>
            <div className={styles.tagChoices}>
              {tags.map((tag) => (
                <label key={tag.id}>
                  <input
                    type="checkbox"
                    checked={
                      mappings[index]?.tagIds.includes(tag.id) ?? false
                    }
                    onChange={(event) =>
                      onMappingsChange((value) =>
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
          disabled={disabled}
          onClick={onStart}
        >
          开始扫描
        </button>
      </footer>
    </section>
  );
}
