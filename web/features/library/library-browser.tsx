"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { ResponsiveSheet } from "@/components/responsive-sheet";
import { PageHeader } from "@/components/ui";
import { useAuth } from "@/features/auth/auth-provider";
import { GameGrid } from "./game-grid";
import {
  gamePageQuery,
  type GamePage,
  type LibraryFacets,
  type LibraryFilters,
} from "./game-library";

const emptyFacets: LibraryFacets = { totalCount: 0, platforms: [], platformInstances: [], tags: [] };

function mergeGames(current: GamePage["items"], incoming: GamePage["items"]) {
  const seen = new Set(current.map((game) => game.gameId));
  return [...current, ...incoming.filter((game) => !seen.has(game.gameId))];
}

export function LibraryBrowser({ initialPage, initialFilters }: { initialPage: GamePage; initialFilters: LibraryFilters }) {
  const { authenticatedFetch } = useAuth();
  const [query, setQuery] = useState(initialFilters.query);
  const [platformId, setPlatformId] = useState(initialFilters.platformId);
  const [platformInstanceId, setPlatformInstanceId] = useState(initialFilters.platformInstanceId);
  const [tagId, setTagId] = useState(initialFilters.tagId);
  const [sort, setSort] = useState<LibraryFilters["sort"]>(initialFilters.sort);
  const [filterOpen, setFilterOpen] = useState(false);
  const [draftPlatformInstanceId, setDraftPlatformInstanceId] = useState(initialFilters.platformInstanceId);
  const [draftTagId, setDraftTagId] = useState(initialFilters.tagId);
  const [draftSort, setDraftSort] = useState<LibraryFilters["sort"]>(initialFilters.sort);
  const [games, setGames] = useState(initialPage.items);
  const [nextCursor, setNextCursor] = useState(initialPage.nextCursor);
  const [filteredCount, setFilteredCount] = useState(initialPage.filteredCount ?? initialPage.items.length);
  const [facets, setFacets] = useState(initialPage.facets ?? emptyFacets);
  const [nowMs, setNowMs] = useState(initialPage.generatedAtMs);
  const [refreshing, setRefreshing] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [refreshVersion, setRefreshVersion] = useState(0);
  const searchRef = useRef<HTMLInputElement>(null);
  const filterButtonRef = useRef<HTMLButtonElement>(null);
  const loadMoreRef = useRef<HTMLDivElement>(null);
  const observedRequest = useRef({ filterKey: gamePageQuery(initialFilters), refreshVersion: 0 });
  const loadMoreRequest = useRef(false);
  const autoLoadArmed = useRef(true);
  const requestGeneration = useRef(0);
  const platformInstances = useMemo(() => facets.platformInstances.filter((item) => !platformId || item.platformId === platformId), [facets.platformInstances, platformId]);
  const hasFilters = [query.trim(), platformId, platformInstanceId, tagId].some(Boolean);

  const filters = useMemo<LibraryFilters>(() => ({ query, platformId, platformInstanceId, tagId, sort }), [platformId, platformInstanceId, query, sort, tagId]);

  const requestPage = useCallback(async (requestedFilters: LibraryFilters, cursor: string | null, signal?: AbortSignal) => {
    const response = await authenticatedFetch(`/api/v1/games?${gamePageQuery(requestedFilters, cursor)}`, { cache: "no-store", signal });
    if (!response.ok) {throw new Error("暂时无法读取游戏库，请稍后重试");}
    return response.json() as Promise<GamePage>;
  }, [authenticatedFetch]);

  useEffect(() => {
    const filterKey = gamePageQuery(filters);
    if (observedRequest.current.filterKey === filterKey && observedRequest.current.refreshVersion === refreshVersion) {
      return;
    }
    observedRequest.current = { filterKey, refreshVersion };
    const generation = requestGeneration.current + 1;
    requestGeneration.current = generation;
    loadMoreRequest.current = false;
    setGames([]);
    setFilteredCount(0);
    setRefreshing(true);
    setLoadError(null);
    const controller = new AbortController();
    const timeout = window.setTimeout(() => {
      void requestPage(filters, null, controller.signal).then((page) => {
        if (requestGeneration.current !== generation) {return;}
        setGames(page.items);
        setNextCursor(page.nextCursor);
        setFilteredCount(page.filteredCount ?? page.items.length);
        if (page.facets) {setFacets(page.facets);}
        setNowMs(page.generatedAtMs);
        autoLoadArmed.current = true;
      }).catch((caught: unknown) => {
        if (caught instanceof DOMException && caught.name === "AbortError") {return;}
        if (requestGeneration.current !== generation) {return;}
        setLoadError(caught instanceof Error ? caught.message : "暂时无法读取游戏库，请稍后重试");
      }).finally(() => {
        if (!controller.signal.aborted && requestGeneration.current === generation) {setRefreshing(false);}
      });
    }, 180);
    return () => {
      window.clearTimeout(timeout);
      controller.abort();
    };
  }, [filters, refreshVersion, requestPage]);

  useEffect(() => {
    const params = new URLSearchParams();
    if (query.trim()) {params.set("q", query.trim());}
    if (platformId) {params.set("platformId", platformId);}
    if (platformInstanceId) {params.set("platformInstanceId", platformInstanceId);}
    if (tagId) {params.set("tagId", tagId);}
    if (sort !== "RECENT_DESC") {params.set("sort", sort);}
    const search = params.toString();
    window.history.replaceState(window.history.state, "", `${window.location.pathname}${search ? `?${search}` : ""}`);
  }, [platformId, platformInstanceId, query, sort, tagId]);

  useEffect(() => {
    const focusSearch = (event: KeyboardEvent) => {
      const target = event.target;
      const editing = target instanceof HTMLElement && (target.matches("input, select, textarea") || target.isContentEditable);
      if (event.key !== "/" || event.metaKey || event.ctrlKey || event.altKey || editing) {return;}
      event.preventDefault();
      searchRef.current?.focus();
    };
    document.addEventListener("keydown", focusSearch);
    return () => document.removeEventListener("keydown", focusSearch);
  }, []);

  function selectPlatform(nextPlatformId: string) {
    setNextCursor(null);
    setPlatformId(nextPlatformId);
    if (!platformInstanceId) {return;}
    const selectedCollection = facets.platformInstances.find((instance) => instance.id === platformInstanceId);
    if (nextPlatformId && selectedCollection?.platformId !== nextPlatformId) {
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
    setNextCursor(null);
    setPlatformInstanceId(draftPlatformInstanceId);
    setTagId(draftTagId);
    setSort(draftSort);
    setFilterOpen(false);
  }

  const mobileFilterCount = Number(Boolean(platformInstanceId)) + Number(Boolean(tagId)) + Number(sort !== "RECENT_DESC");

  const loadMore = useCallback(async () => {
    if (!nextCursor || loadMoreRequest.current) {return;}
    loadMoreRequest.current = true;
    setLoadingMore(true);
    setLoadError(null);
    const generation = requestGeneration.current;
    try {
      const page = await requestPage(filters, nextCursor);
      if (requestGeneration.current !== generation) {return;}
      setGames((current) => mergeGames(current, page.items));
      setNextCursor(page.nextCursor);
      setNowMs(page.generatedAtMs);
    } catch (caught) {
      if (requestGeneration.current !== generation) {return;}
      setLoadError(caught instanceof Error ? caught.message : "暂时无法读取下一页，请稍后重试");
    } finally {
      if (requestGeneration.current === generation) {
        loadMoreRequest.current = false;
        setLoadingMore(false);
      }
    }
  }, [filters, nextCursor, requestPage]);

  useEffect(() => {
    const target = loadMoreRef.current;
    if (!target || !nextCursor || typeof IntersectionObserver === "undefined") {return;}
    const observer = new IntersectionObserver((entries) => {
      const intersects = entries.some((entry) => entry.isIntersecting);
      if (!intersects) {
        autoLoadArmed.current = true;
        return;
      }
      if (!autoLoadArmed.current) {return;}
      autoLoadArmed.current = false;
      void loadMore();
    }, { rootMargin: "600px 0px" });
    observer.observe(target);
    return () => observer.disconnect();
  }, [loadMore, nextCursor]);

  return <div className="page-layout page-layout-library">
    <PageHeader eyebrow="我的游戏" title="游戏库" description="找到想玩的经典游戏，打开详情后即可使用推荐配置开始游玩。" />

    <section className="library-toolbar" aria-label="游戏筛选">
      <div className="library-tool-row">
        <label className="library-search" htmlFor="library-search"><span className="sr-only">搜索游戏</span><AppIcon name="search" /><input ref={searchRef} id="library-search" type="search" value={query} onChange={(event) => { setNextCursor(null); setQuery(event.target.value); }} placeholder="搜索游戏、平台或标签" /></label>
        <label className="library-desktop-filter"><span className="sr-only">游戏集合</span><select aria-label="游戏集合" value={platformInstanceId} onChange={(event) => { setNextCursor(null); setPlatformInstanceId(event.target.value); }}><option value="">所有游戏集合</option>{platformInstances.map((instance) => <option value={instance.id} key={instance.id}>{instance.name}</option>)}</select></label>
        <label className="library-desktop-filter"><span className="sr-only">标签</span><select aria-label="标签" value={tagId} onChange={(event) => { setNextCursor(null); setTagId(event.target.value); }}><option value="">所有标签</option>{facets.tags.map((tag) => <option value={tag.id} key={tag.id}>{tag.name} · {tag.count}</option>)}</select></label>
        <label className="library-desktop-filter"><span className="sr-only">排列顺序</span><select aria-label="排列顺序" value={sort} onChange={(event) => { setNextCursor(null); setSort(event.target.value as LibraryFilters["sort"]); }}><option value="RECENT_DESC">最近游玩</option><option value="ADDED_DESC">最近加入</option><option value="TITLE_ASC">名称 A–Z</option></select></label>
        <button ref={filterButtonRef} className="button secondary library-mobile-filter-trigger" type="button" aria-expanded={filterOpen} onClick={openFilters}><AppIcon name="settings" />筛选与排序{mobileFilterCount ? ` · ${mobileFilterCount}` : ""}</button>
      </div>
      <div className="library-platform-row">
        <span className="library-platform-label">游戏平台</span>
        <button className={!platformId ? "is-active" : ""} type="button" aria-pressed={!platformId} onClick={() => selectPlatform("")}>全部 <strong>{facets.totalCount}</strong></button>
        {facets.platforms.map((platform) => <button className={platform.id === platformId ? "is-active" : ""} type="button" aria-pressed={platform.id === platformId} onClick={() => selectPlatform(platform.id)} key={platform.id}>{platform.name} <strong>{platform.count}</strong></button>)}
        <span className="library-result-count" aria-live="polite">已加载 <strong>{games.length}</strong> / {filteredCount} 款游戏</span>
      </div>
    </section>

    <ResponsiveSheet open={filterOpen} title="筛选与排序" description={`当前匹配 ${filteredCount} 款游戏`} placement="bottom" onClose={() => setFilterOpen(false)} returnFocusRef={filterButtonRef} footer={<><button className="button secondary" type="button" onClick={() => setFilterOpen(false)}>取消</button><button className="button" type="button" onClick={applyFilters}>应用</button></>}>
      <div className="mobile-filter-fields">
        <label><span>游戏集合</span><select aria-label="游戏集合" value={draftPlatformInstanceId} onChange={(event) => setDraftPlatformInstanceId(event.target.value)}><option value="">所有游戏集合</option>{platformInstances.map((instance) => <option value={instance.id} key={instance.id}>{instance.name}</option>)}</select></label>
        <label><span>标签</span><select aria-label="标签" value={draftTagId} onChange={(event) => setDraftTagId(event.target.value)}><option value="">所有标签</option>{facets.tags.map((tag) => <option value={tag.id} key={tag.id}>{tag.name} · {tag.count}</option>)}</select></label>
        <label><span>排列顺序</span><select aria-label="排列顺序" value={draftSort} onChange={(event) => setDraftSort(event.target.value as LibraryFilters["sort"])}><option value="RECENT_DESC">最近游玩</option><option value="ADDED_DESC">最近加入</option><option value="TITLE_ASC">名称 A–Z</option></select></label>
        {mobileFilterCount ? <button className="button secondary" type="button" onClick={() => { setDraftPlatformInstanceId(""); setDraftTagId(""); setDraftSort("RECENT_DESC"); }}>清除全部</button> : null}
      </div>
    </ResponsiveSheet>

    <div className="library-section-head"><div><h2>所有游戏</h2><p>封面是识别入口；没有封面的游戏也保留清晰的标题与平台信息。</p></div><span>卡片视图 · <kbd>/</kbd> 搜索</span></div>
    {refreshing ? <div className="library-loading" role="status">正在更新游戏列表…</div> : loadError && !nextCursor ? null : <GameGrid games={games} nowMs={nowMs} filtered={hasFilters} />}
    <div ref={loadMoreRef} className="infinite-scroll-sentinel" aria-hidden="true" />
    <footer className="library-pagination" aria-live="polite">
      <span>{refreshing ? "正在更新游戏列表…" : `已加载 ${games.length} / ${filteredCount} 款游戏`}</span>
      {loadError ? <button className="button secondary compact" type="button" onClick={() => nextCursor ? void loadMore() : setRefreshVersion((value) => value + 1)}>{loadError}，点击重试</button> : loadingMore ? <span role="status">正在加载下一页…</span> : nextCursor ? <button className="button secondary compact" type="button" onClick={() => void loadMore()}>继续加载</button> : <span>已加载当前条件的全部游戏</span>}
    </footer>
  </div>;
}
