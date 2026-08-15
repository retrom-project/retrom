"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import { PageHeader } from "@/components/ui";
import { GameGrid } from "./game-grid";
import {
  filterLibraryGames,
  libraryPlatformInstances,
  libraryPlatforms,
  libraryTags,
  type GameSummary,
  type LibraryFilters,
} from "./game-library";

export function LibraryBrowser({ games, nowMs, initialFilters }: { games: GameSummary[]; nowMs: number; initialFilters: LibraryFilters }) {
  const [query, setQuery] = useState(initialFilters.query);
  const [platformId, setPlatformId] = useState(initialFilters.platformId);
  const [platformInstanceId, setPlatformInstanceId] = useState(initialFilters.platformInstanceId);
  const [tagId, setTagId] = useState(initialFilters.tagId);
  const [sort, setSort] = useState<LibraryFilters["sort"]>(initialFilters.sort);
  const [filterOpen, setFilterOpen] = useState(false);
  const [draftPlatformInstanceId, setDraftPlatformInstanceId] = useState(initialFilters.platformInstanceId);
  const [draftTagId, setDraftTagId] = useState(initialFilters.tagId);
  const [draftSort, setDraftSort] = useState<LibraryFilters["sort"]>(initialFilters.sort);
  const searchRef = useRef<HTMLInputElement>(null);
  const filterButtonRef = useRef<HTMLButtonElement>(null);
  const platforms = useMemo(() => libraryPlatforms(games), [games]);
  const platformInstances = useMemo(() => libraryPlatformInstances(games, platformId), [games, platformId]);
  const tags = useMemo(() => libraryTags(games), [games]);
  const filteredGames = useMemo(() => filterLibraryGames(games, { query, platformId, platformInstanceId, tagId, sort }), [games, platformId, platformInstanceId, query, sort, tagId]);
  const hasFilters = Boolean(query.trim() || platformId || platformInstanceId || tagId);

  useEffect(() => {
    const params = new URLSearchParams();
    if (query.trim()) params.set("q", query.trim());
    if (platformId) params.set("platformId", platformId);
    if (platformInstanceId) params.set("platformInstanceId", platformInstanceId);
    if (tagId) params.set("tagId", tagId);
    if (sort !== "RECENT_DESC") params.set("sort", sort);
    const search = params.toString();
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${search ? `?${search}` : ""}`);
  }, [platformId, platformInstanceId, query, sort, tagId]);

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
    if (nextPlatformId && selectedCollection?.platform.id !== nextPlatformId) {
      setPlatformInstanceId("");
      setDraftPlatformInstanceId("");
    }
  }

  function openFilters() {
    setDraftPlatformInstanceId(platformInstanceId);
    setDraftTagId(tagId);
    setDraftSort(sort);
    setFilterOpen(true);
  }

  function applyFilters() {
    setPlatformInstanceId(draftPlatformInstanceId);
    setTagId(draftTagId);
    setSort(draftSort);
    setFilterOpen(false);
  }

  const mobileFilterCount = Number(Boolean(platformInstanceId)) + Number(Boolean(tagId)) + Number(sort !== "RECENT_DESC");

  return <div className="page-layout page-layout-library">
    <PageHeader eyebrow="我的游戏" title="游戏库" description="找到想玩的经典游戏，打开详情后即可使用推荐配置开始游玩。" />

    <section className="library-toolbar" aria-label="游戏筛选">
      <div className="library-tool-row">
        <label className="library-search" htmlFor="library-search"><span className="sr-only">搜索游戏</span><AppIcon name="search" /><input ref={searchRef} id="library-search" type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索游戏、平台或标签" /></label>
        <label className="library-desktop-filter"><span className="sr-only">游戏集合</span><select aria-label="游戏集合" value={platformInstanceId} onChange={(event) => setPlatformInstanceId(event.target.value)}><option value="">所有游戏集合</option>{platformInstances.map((instance) => <option value={instance.id} key={instance.id}>{instance.name}</option>)}</select></label>
        <label className="library-desktop-filter"><span className="sr-only">标签</span><select aria-label="标签" value={tagId} onChange={(event) => setTagId(event.target.value)}><option value="">所有标签</option>{tags.map((tag) => <option value={tag.tagId} key={tag.tagId}>{tag.name} · {tag.count}</option>)}</select></label>
        <label className="library-desktop-filter"><span className="sr-only">排列顺序</span><select aria-label="排列顺序" value={sort} onChange={(event) => setSort(event.target.value as LibraryFilters["sort"])}><option value="RECENT_DESC">最近游玩</option><option value="ADDED_DESC">最近加入</option><option value="TITLE_ASC">名称 A–Z</option></select></label>
        <button ref={filterButtonRef} className="button secondary library-mobile-filter-trigger" type="button" aria-expanded={filterOpen} onClick={openFilters}><AppIcon name="settings" />筛选与排序{mobileFilterCount ? ` · ${mobileFilterCount}` : ""}</button>
      </div>
      <div className="library-platform-row">
        <span className="library-platform-label">游戏平台</span>
        <button className={!platformId ? "is-active" : ""} type="button" aria-pressed={!platformId} onClick={() => selectPlatform("")}>全部 <strong>{games.length}</strong></button>
        {platforms.map((platform) => <button className={platform.id === platformId ? "is-active" : ""} type="button" aria-pressed={platform.id === platformId} onClick={() => selectPlatform(platform.id)} key={platform.id}>{platform.name} <strong>{platform.count}</strong></button>)}
        <span className="library-result-count" aria-live="polite">当前显示 <strong>{filteredGames.length}</strong> 款游戏</span>
      </div>
    </section>

    <ResponsiveSheet open={filterOpen} title="筛选与排序" description={`当前显示 ${filteredGames.length} 款游戏`} placement="bottom" onClose={() => setFilterOpen(false)} returnFocusRef={filterButtonRef} footer={<><button className="button secondary" type="button" onClick={() => setFilterOpen(false)}>取消</button><button className="button" type="button" onClick={applyFilters}>应用</button></>}>
      <div className="mobile-filter-fields">
        <label><span>游戏集合</span><select aria-label="游戏集合" value={draftPlatformInstanceId} onChange={(event) => setDraftPlatformInstanceId(event.target.value)}><option value="">所有游戏集合</option>{platformInstances.map((instance) => <option value={instance.id} key={instance.id}>{instance.name}</option>)}</select></label>
        <label><span>标签</span><select aria-label="标签" value={draftTagId} onChange={(event) => setDraftTagId(event.target.value)}><option value="">所有标签</option>{tags.map((tag) => <option value={tag.tagId} key={tag.tagId}>{tag.name} · {tag.count}</option>)}</select></label>
        <label><span>排列顺序</span><select aria-label="排列顺序" value={draftSort} onChange={(event) => setDraftSort(event.target.value as LibraryFilters["sort"])}><option value="RECENT_DESC">最近游玩</option><option value="ADDED_DESC">最近加入</option><option value="TITLE_ASC">名称 A–Z</option></select></label>
        {mobileFilterCount ? <button className="button secondary" type="button" onClick={() => { setDraftPlatformInstanceId(""); setDraftTagId(""); setDraftSort("RECENT_DESC"); }}>清除全部</button> : null}
      </div>
    </ResponsiveSheet>

    <div className="library-section-head"><div><h2>所有游戏</h2><p>封面是识别入口；没有封面的游戏也保留清晰的标题与平台信息。</p></div><span>卡片视图 · <kbd>/</kbd> 搜索</span></div>
    <GameGrid games={filteredGames} nowMs={nowMs} filtered={hasFilters} />
  </div>;
}
