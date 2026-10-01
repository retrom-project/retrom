"use client";

import { useState } from "react";
import { formatBytes } from "@/lib/backend";
import type { GameFile } from "./admin-game-manager";

const pageSize = 20;
const fileRoles: Record<string, string> = {
  CONTENT: "游戏主文件",
  COMPANION: "依赖文件 / Parent ROM",
  DISC: "光盘",
  PLAYLIST_SOURCE: "播放列表",
  DOS_SOURCE: "DOS 来源文件",
  PROJECT_FILE: "项目文件",
  RPG_EASYRPG_INDEX: "项目索引",
  RPG_MAKER_LAUNCH_BUNDLE: "运行文件包",
};

const fileRoleOrder: Record<string, number> = { CONTENT: 0, PLAYLIST_SOURCE: 0, DISC: 1, COMPANION: 2 };

function compareFiles(left: GameFile, right: GameFile) {
  return (fileRoleOrder[left.role] ?? 3) - (fileRoleOrder[right.role] ?? 3)
    || left.sortOrder - right.sortOrder || left.logicalName.localeCompare(right.logicalName);
}

function FileMetadata({ file }: { file: GameFile }) {
  const hashes = [["SHA-256", file.sha256], ["MD5", file.md5], ["SHA-1", file.sha1], ["CRC32", file.crc32]];
  return <dl className="admin-game-file-metadata">
    <div><dt>大小</dt><dd>{formatBytes(file.sizeBytes)} <span>（{file.sizeBytes.toLocaleString("zh-CN")} 字节）</span></dd></div>
    {file.mediaType ? <div><dt>媒体类型</dt><dd>{file.mediaType}</dd></div> : null}
    {hashes.filter(([, value]) => value).map(([label, value]) => <div key={label}><dt>{label}</dt><dd><code>{value}</code></dd></div>)}
  </dl>;
}

export function AdminGameFiles({ files }: { files: GameFile[] }) {
  const [page, setPage] = useState(0);
  const ordered = [...files].sort(compareFiles);
  const lastPage = Math.max(0, Math.ceil(ordered.length / pageSize) - 1);
  const currentPage = Math.min(page, lastPage);
  const visible = ordered.slice(currentPage * pageSize, (currentPage + 1) * pageSize);
  const discs = ordered.filter((file) => file.role === "DISC");

  return <section className="panel admin-game-files" aria-labelledby="admin-game-files-title">
    <div className="panel-head"><h2 id="admin-game-files-title">游戏文件</h2><span>{files.length} 个文件</span></div>
    <div className="panel-body">
      {visible.length ? <ul className="admin-game-file-list">{visible.map((file) => <li key={`${file.role}:${file.logicalName}`}>
        <div className="admin-game-file-heading"><strong>{file.logicalName}</strong><span>{file.role === "DISC" ? `光盘 ${discs.indexOf(file) + 1}` : fileRoles[file.role] ?? file.role}</span></div>
        <FileMetadata file={file} />
      </li>)}</ul> : <p className="muted">暂无游戏文件</p>}
      {lastPage > 0 ? <nav className="admin-game-file-pagination" aria-label="游戏文件分页">
        <button className="button secondary" type="button" disabled={currentPage === 0} onClick={() => setPage(currentPage - 1)}>上一页</button>
        <span>第 {currentPage + 1} / {lastPage + 1} 页</span>
        <button className="button secondary" type="button" disabled={currentPage === lastPage} onClick={() => setPage(currentPage + 1)}>下一页</button>
      </nav> : null}
    </div>
  </section>;
}
