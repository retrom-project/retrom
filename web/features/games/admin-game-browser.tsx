"use client";

import Image from "next/image";
import Link from "next/link";
import { useEffect, useMemo, useRef, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { EmptyState, StatusBadge } from "@/components/ui";
import {
  adminGameDirectories,
  adminGamePlatforms,
  adminGameSummary,
  adminGameUpdateNote,
  filterAdminGames,
  formatAdminGameTime,
  runtimePresentation,
  type AdminGameFilters,
  type AdminGameSummary,
} from "./admin-game-library";

const PAGE_SIZE = 6;

function updateLocation(filters: AdminGameFilters) {
  const query = new URLSearchParams();
  if (filters.query.trim()) query.set("q", filters.query.trim());
  if (filters.platformId) query.set("platformId", filters.platformId);
  if (filters.platformInstanceId) query.set("platformInstanceId", filters.platformInstanceId);
  if (filters.visibility !== "ALL") query.set("status", filters.visibility);
  if (filters.runtime !== "ALL") query.set("runtime", filters.runtime);
  if (filters.sort !== "UPDATED_DESC") query.set("sort", filters.sort);
  const encoded = query.toString();
  window.history.replaceState(window.history.state, "", encoded ? `${window.location.pathname}?${encoded}` : window.location.pathname);
}

function csvCell(value: string | number | null) {
  return `"${String(value ?? "").replaceAll('"', '""')}"`;
}

function exportGames(games: AdminGameSummary[]) {
  const rows = [
    ["游戏", "平台", "游戏目录", "推荐运行方式", "发行年份", "用户状态", "运行状态", "最近更新"],
    ...games.map((game) => [game.title, game.platform.name, game.platformInstance.name, game.defaultCore.name, game.releaseYear, game.status, game.runtimeStatus ?? "PENDING", game.updatedAtMs]),
  ];
  const blob = new Blob([`\uFEFF${rows.map((row) => row.map(csvCell).join(",")).join("\n")}`], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = "retrom-admin-games.csv";
  anchor.click();
  URL.revokeObjectURL(url);
}

export function AdminGameBrowser({ games, nowMs, initialFilters }: { games: AdminGameSummary[]; nowMs: number; initialFilters: AdminGameFilters }) {
  const searchRef = useRef<HTMLInputElement>(null);
  const [filters, setFilters] = useState(initialFilters);
  const [pageIndex, setPageIndex] = useState(0);
  const summary = useMemo(() => adminGameSummary(games), [games]);
  const platforms = useMemo(() => adminGamePlatforms(games), [games]);
  const directories = useMemo(() => adminGameDirectories(games, filters.platformId), [games, filters.platformId]);
  const filtered = useMemo(() => filterAdminGames(games, filters), [games, filters]);
  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const currentPage = Math.min(pageIndex, pageCount - 1);
  const visibleGames = filtered.slice(currentPage * PAGE_SIZE, (currentPage + 1) * PAGE_SIZE);

  useEffect(() => {
    const focusSearch = (event: KeyboardEvent) => {
      if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey) return;
      const target = event.target;
      if (target instanceof HTMLInputElement || target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement) return;
      event.preventDefault();
      searchRef.current?.focus();
    };
    document.addEventListener("keydown", focusSearch);
    return () => document.removeEventListener("keydown", focusSearch);
  }, []);

  function change(next: Partial<AdminGameFilters>) {
    setFilters((current) => {
      const updated = { ...current, ...next };
      if (Object.hasOwn(next, "platformId") && updated.platformInstanceId) {
        const selected = games.find((game) => game.platformInstance.id === updated.platformInstanceId);
        if (!selected || selected.platform.id !== updated.platformId) updated.platformInstanceId = "";
      }
      updateLocation(updated);
      return updated;
    });
    setPageIndex(0);
  }

  const summaryItems = [
    { label: "全部游戏", value: summary.total, tone: "active" },
    { label: "运行异常", value: summary.runtimeAttention, tone: "warn" },
    { label: "缺少封面", value: summary.missingCover, tone: "neutral" },
    { label: "资料不完整", value: summary.incompleteMetadata, tone: "neutral" },
    { label: "用户不可见", value: summary.hidden, tone: "bad" },
  ];

  return <div className="admin-game-browser">
    <button className="button secondary admin-game-export" type="button" onClick={() => exportGames(filtered)}><AppIcon name="download" />导出当前列表</button>
    <section className="admin-game-summary" aria-label="游戏管理摘要">
      {summaryItems.map((item) => <article className={item.tone} key={item.label}><span>{item.label}</span><strong>{item.value}</strong></article>)}
    </section>
    <section className="admin-game-toolbar" aria-label="筛选游戏">
      <label className="admin-game-search"><span>搜索游戏</span><span><AppIcon name="search" /><input ref={searchRef} type="search" value={filters.query} placeholder="输入游戏名称" onChange={(event) => change({ query: event.target.value })} /></span></label>
      <label><span>平台</span><select value={filters.platformId} onChange={(event) => change({ platformId: event.target.value })}><option value="">所有平台</option>{platforms.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
      <label><span>游戏目录</span><select value={filters.platformInstanceId} onChange={(event) => change({ platformInstanceId: event.target.value })}><option value="">所有目录</option>{directories.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
      <label><span>用户状态</span><select value={filters.visibility} onChange={(event) => change({ visibility: event.target.value as AdminGameFilters["visibility"] })}><option value="ALL">全部状态</option><option value="PUBLISHED">用户可见</option><option value="DELETED">用户不可见</option></select></label>
      <label><span>运行状态</span><select value={filters.runtime} onChange={(event) => change({ runtime: event.target.value as AdminGameFilters["runtime"] })}><option value="ALL">全部状态</option><option value="READY">可以运行</option><option value="ATTENTION">需要处理</option></select></label>
      <label><span>排序</span><select value={filters.sort} onChange={(event) => change({ sort: event.target.value as AdminGameFilters["sort"] })}><option value="UPDATED_DESC">最近更新</option><option value="ADDED_DESC">最近加入</option><option value="TITLE_ASC">名称排序</option></select></label>
    </section>
    {visibleGames.length === 0 ? <EmptyState title="没有可管理的游戏" description="当前搜索和筛选条件没有匹配项，请调整后重试。" /> : <div className="admin-game-table-scroll" tabIndex={0} aria-label="游戏管理表格，可横向滚动">
      <table className="admin-game-table">
        <thead><tr><th>封面</th><th>游戏</th><th>用户状态</th><th>运行状态</th><th>运行环境 / 目录</th><th>最近更新</th><th>操作</th></tr></thead>
        <tbody>{visibleGames.map((game) => {
          const runtime = runtimePresentation(game.runtimeStatus);
          return <tr key={game.gameId}>
            <td><div className="admin-game-thumb">{game.coverUrl ? <Image src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="70px" unoptimized /> : <span role="img" aria-label={`${game.title} 暂无封面`}><strong>{game.title}</strong><small>{game.platform.name}</small></span>}</div></td>
            <td><div className="admin-game-identity"><Link href={`/admin/games/${game.gameId}`}>{game.title}</Link><p>{game.platform.name}{game.releaseYear ? ` · ${game.releaseYear}` : ""}</p><span>{game.platformInstance.name}</span></div></td>
            <td className="admin-game-visibility"><StatusBadge tone={game.status === "PUBLISHED" ? "good" : "bad"}>{game.status === "PUBLISHED" ? "用户可见" : "用户不可见"}</StatusBadge></td>
            <td><StatusBadge tone={runtime.tone}>{runtime.label}</StatusBadge></td>
            <td><strong>{game.platformInstance.name}</strong><small>{game.platform.name} · 推荐 {game.defaultCore.name}</small></td>
            <td><strong>{formatAdminGameTime(game.updatedAtMs, nowMs)}</strong><small>{adminGameUpdateNote(game)}</small></td>
            <td><Link className="admin-game-manage" href={`/admin/games/${game.gameId}`}>管理 →</Link></td>
          </tr>;
        })}</tbody>
      </table>
    </div>}
    <footer className="admin-game-pagination"><span>当前展示 {visibleGames.length} / {filtered.length} 款游戏</span><div><button type="button" disabled={currentPage === 0} onClick={() => setPageIndex((value) => Math.max(0, value - 1))}>上一页</button><span>第 {currentPage + 1} 页 · 共 {pageCount} 页</span><button type="button" disabled={currentPage >= pageCount - 1} onClick={() => setPageIndex((value) => Math.min(pageCount - 1, value + 1))}>下一页</button></div></footer>
  </div>;
}
