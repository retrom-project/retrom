"use client";
import { useState } from "react";
import type { Schema } from "@/lib/api/types";
export function GameFileList({ files }: { files: Schema<"GameFile">[] }) {
  const [query, setQuery] = useState("");
  const [offset, setOffset] = useState(0);
  const filtered = files.filter((file) =>
    file.logicalKey.toLocaleLowerCase().includes(query.toLocaleLowerCase()),
  );
  return (
    <details>
      <summary>内容文件（{files.length}）</summary>
      <div className="stack">
        <label className="field">
          搜索文件
          <input
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setOffset(0);
            }}
          />
        </label>
        <ul className="game-file-list">
          {filtered.slice(offset, offset + 30).map((file) => (
            <li key={file.id}>
              <span>{file.logicalKey}</span>
              <small>
                {(file.sizeBytes / 1024).toFixed(1)} KiB ·{" "}
                {file.role === "parent" ? "Parent" : "内容"}
              </small>
            </li>
          ))}
        </ul>
        <div className="workspace-paging">
          <span>匹配 {filtered.length} 个文件</span>
          <button
            className="button secondary"
            disabled={!offset}
            onClick={() => setOffset(Math.max(0, offset - 30))}
          >
            上一页
          </button>
          <button
            className="button secondary"
            disabled={offset + 30 >= filtered.length}
            onClick={() => setOffset(offset + 30)}
          >
            下一页
          </button>
        </div>
      </div>
    </details>
  );
}
