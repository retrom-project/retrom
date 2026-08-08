"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { PageHeader } from "@/components/ui";
import { GameGrid } from "./game-grid";
import {
  filterLibraryGames,
  libraryCollections,
  libraryPlatforms,
  type GameSummary,
  type LibraryFilters,
} from "./game-library";

export function LibraryBrowser({ games, nowMs, initialFilters }: { games: GameSummary[]; nowMs: number; initialFilters: LibraryFilters }) {
  const [query, setQuery] = useState(initialFilters.query);
  const [platformId, setPlatformId] = useState(initialFilters.platformId);
  const [platformInstanceId, setPlatformInstanceId] = useState(initialFilters.platformInstanceId);
  const [sort, setSort] = useState<LibraryFilters["sort"]>(initialFilters.sort);
  const searchRef = useRef<HTMLInputElement>(null);
  const platforms = useMemo(() => libraryPlatforms(games), [games]);
  const collections = useMemo(() => libraryCollections(games, platformId), [games, platformId]);
  const filteredGames = useMemo(() => filterLibraryGames(games, { query, platformId, platformInstanceId, sort }), [games, platformId, platformInstanceId, query, sort]);
  const hasFilters = Boolean(query.trim() || platformId || platformInstanceId);

  useEffect(() => {
    const params = new URLSearchParams();
    if (query.trim()) params.set("q", query.trim());
    if (platformId) params.set("platformId", platformId);
    if (platformInstanceId) params.set("platformInstanceId", platformInstanceId);
    if (sort !== "RECENT_DESC") params.set("sort", sort);
    const search = params.toString();
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${search ? `?${search}` : ""}`);
  }, [platformId, platformInstanceId, query, sort]);

  useEffect(() => {
    const focusSearch = (event: KeyboardEvent) => {
      const target = event.target;
      const editing = target instanceof HTMLElement && (target.matches("input, select, textarea") || target.isContentEditable);
      if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey || editing) return;
      event.preventDefault();
      searchRef.current?.focus();
    };
    document.addEventListener("keydown", focusSearch);
    return () => document.removeEventListener("keydown", focusSearch);
  }, []);

  function selectPlatform(nextPlatformId: string) {
    setPlatformId(nextPlatformId);
    if (!platformInstanceId) return;
    const selectedCollection = games.find((game) => game.platformInstance.id === platformInstanceId);
    if (nextPlatformId && selectedCollection?.platform.id !== nextPlatformId) setPlatformInstanceId("");
  }

  return <div className="page-layout page-layout-library">
    <PageHeader eyebrow="我的游戏" title="游戏库" description="找到想玩的经典游戏，打开详情后即可使用推荐配置开始游玩。" />

    <section className="library-toolbar" aria-label="游戏筛选">
      <div className="library-tool-row">
        <label className="library-search" htmlFor="library-search"><span className="sr-only">搜索游戏</span><AppIcon name="search" /><input ref={searchRef} id="library-search" type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索游戏" /></label>
        <label><span className="sr-only">游戏集合</span><select aria-label="游戏集合" value={platformInstanceId} onChange={(event) => setPlatformInstanceId(event.target.value)}><option value="">所有游戏集合</option>{collections.map((collection) => <option value={collection.id} key={collection.id}>{collection.name}</option>)}</select></label>
        <label><span className="sr-only">排列顺序</span><select aria-label="排列顺序" value={sort} onChange={(event) => setSort(event.target.value as LibraryFilters["sort"])}><option value="RECENT_DESC">最近游玩</option><option value="ADDED_DESC">最近加入</option><option value="TITLE_ASC">名称 A–Z</option></select></label>
      </div>
      <div className="library-platform-row">
        <span className="library-platform-label">游戏平台</span>
        <button className={!platformId ? "is-active" : ""} type="button" aria-pressed={!platformId} onClick={() => selectPlatform("")}>全部 <strong>{games.length}</strong></button>
        {platforms.map((platform) => <button className={platform.id === platformId ? "is-active" : ""} type="button" aria-pressed={platform.id === platformId} onClick={() => selectPlatform(platform.id)} key={platform.id}>{platform.name} <strong>{platform.count}</strong></button>)}
        <span className="library-result-count">当前显示 <strong>{filteredGames.length}</strong> 款游戏</span>
      </div>
    </section>

    <div className="library-section-head"><div><h2>所有游戏</h2><p>封面是识别入口；没有封面的游戏也保留清晰的标题与平台信息。</p></div><span>卡片视图 · <kbd>/</kbd> 搜索</span></div>
    <GameGrid games={filteredGames} nowMs={nowMs} filtered={hasFilters} />
  </div>;
}
