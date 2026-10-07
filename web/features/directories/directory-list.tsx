"use client";
import { useHorizontalWheel } from "@/lib/use-horizontal-wheel";
import { useState } from "react";
import type { Directory, Schema } from "@/lib/api/types";
import { AppIcon } from "@/components/app-icon";
import { directoryCategories, categoryForPlatform } from "./platform-category";
import { DirectoryMenu } from "./directory-menu";
export function DirectoryList({
  directories,
  catalog,
  onEdit,
  onDelete,
}: {
  directories: Directory[];
  catalog: Schema<"RuntimeCatalog"> | null;
  onEdit: (directory: Directory) => void;
  onDelete: (directory: Directory) => void;
}) {
  const [query, setQuery] = useState("");
  const [platform, setPlatform] = useState("");
  const [status, setStatus] = useState("");
  const [open, setOpen] = useState<string[]>([]);
  const filtered = directories.filter(
    (directory) =>
      (!platform || directory.platformId === platform) &&
      (!status || String(directory.enabled) === status) &&
      `${directory.name} ${directory.description} ${directory.slug} ${directory.platformId}`
        .toLocaleLowerCase()
        .includes(query.toLocaleLowerCase()),
  );
  const groups = directoryCategories
    .map((category) => ({
      ...category,
      items: filtered.filter(
        (directory) =>
          categoryForPlatform(directory.platformId) === category.id,
      ),
    }))
    .filter((group) => group.items.length);
  return (
    <>
      <div className="platform-directory-toolbar">
        <label className="platform-directory-search">
          搜索目录
          <span>
            <AppIcon name="search" />
            <input
              value={query}
              placeholder="输入目录名称、平台或说明"
              onChange={(event) => setQuery(event.target.value)}
            />
          </span>
        </label>
        <label>
          游戏平台
          <select
            value={platform}
            aria-label="游戏平台"
            onChange={(event) => setPlatform(event.target.value)}
          >
            <option value="">所有平台</option>
            {catalog?.platforms
              .filter((item) =>
                directories.some(
                  (directory) => directory.platformId === item.id,
                ),
              )
              .map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
          </select>
        </label>
        <label>
          启用状态
          <select
            value={status}
            aria-label="启用状态"
            onChange={(event) => setStatus(event.target.value)}
          >
            <option value="">全部状态</option>
            <option value="true">启用</option>
            <option value="false">停用</option>
          </select>
        </label>
      </div>
      <div className="platform-directory-group-actions">
        <span>按平台类型浏览目录</span>
        <div>
          <button
            className="button secondary"
            onClick={() => setOpen(groups.map((group) => group.id))}
          >
            全部展开
          </button>
          <button className="button secondary" onClick={() => setOpen([])}>
            全部收起
          </button>
        </div>
      </div>
      <div className="platform-directory-groups">
        {groups.map((group) => (
          <section className="platform-directory-group" key={group.id}>
            <h2>
              <button
                className="button secondary platform-directory-group-toggle"
                aria-expanded={open.includes(group.id)}
                onClick={() =>
                  setOpen((value) =>
                    value.includes(group.id)
                      ? value.filter((id) => id !== group.id)
                      : [...value, group.id],
                  )
                }
              >
                <AppIcon name="chevron-down" />
                {group.label}
                <small>{group.items.length} 个目录</small>
              </button>
            </h2>
            {open.includes(group.id) ? (
              <DirectoryTable
                directories={group.items}
                catalog={catalog}
                onEdit={onEdit}
                onDelete={onDelete}
              />
            ) : null}
          </section>
        ))}
      </div>
      <div className="platform-directory-footer">
        <span>
          匹配 {filtered.length} / {directories.length} 个目录
        </span>
        <span>同一平台可创建多个游戏目录</span>
      </div>
    </>
  );
}
function DirectoryTable({
  directories,
  catalog,
  onEdit,
  onDelete,
}: {
  directories: Directory[];
  catalog: Schema<"RuntimeCatalog"> | null;
  onEdit: (directory: Directory) => void;
  onDelete: (directory: Directory) => void;
}) {
  const tableRail = useHorizontalWheel<HTMLDivElement>();
  return (
    <div
      ref={tableRail} className="platform-directory-table-scroll"
      tabIndex={0}
      role="region"
      aria-label="游戏目录表"
    >
      <div className="platform-directory-table">
        <div className="platform-directory-table-head">
          <span>游戏目录</span>
          <span>游戏平台</span>
          <span>标识</span>
          <span>游戏</span>
          <span>默认核心</span>
          <span>状态</span>
          <span>操作</span>
        </div>
        {directories.map((directory) => (
          <article className="platform-directory-row" key={directory.id}>
            <div className="platform-directory-copy">
              <h3>{directory.name}</h3>
              <p>{directory.description || "暂无说明"}</p>
            </div>
            <div className="platform-directory-platform">
              <strong>
                {catalog?.platforms.find(
                  (platform) => platform.id === directory.platformId,
                )?.name ?? directory.platformId}
              </strong>
              <small>{directory.platformId}</small>
            </div>
            <span>{directory.slug}</span>
            <div className="platform-directory-games">
              <strong>{directory.gameCount}</strong>
              <small>款游戏</small>
            </div>
            <div>
              <strong>
                {catalog?.cores.find(
                  (core) => core.id === directory.defaultCoreId,
                )?.name ?? directory.defaultCoreId}
              </strong>
              <p className="workspace-note">
                {directory.coreIds.length} 个可用核心
              </p>
            </div>
            <span
              className={`status ${directory.enabled ? "good" : "neutral"}`}
            >
              {directory.enabled ? "启用" : "停用"}
            </span>
            <DirectoryMenu
              directory={directory}
              onEdit={onEdit}
              onDelete={onDelete}
            />
          </article>
        ))}
      </div>
    </div>
  );
}
