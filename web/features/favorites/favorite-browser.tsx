"use client";

import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { AppIcon } from "@/components/app-icon";
import { PageHeader } from "@/components/ui";
import { useAuth } from "@/features/auth/auth-provider";
import {
  createFavoriteFolder,
  deleteFavoriteFolder,
  FavoriteAPIError,
  loadFavorites,
  organizeFavorites,
  renameFavoriteFolder,
  restoreFavorites,
  unfavoriteGames,
  type FavoritePage,
  type FavoriteReference,
  type UnfavoriteResult,
} from "./favorite-api";
import { FavoriteGrid } from "./favorite-grid";
import { favoriteQueryString, selectFavoriteScope, toggleGameSelection, type FavoriteQuery } from "./favorite-state";
import { FolderEditDialog, FolderNameDialog, FolderPickerDialog } from "./folder-dialogs";

type ToastState = { message: string; undo?: UnfavoriteResult["items"] };

function errorMessage(error: unknown) {
  if (error instanceof DOMException && error.name === "AbortError") return "";
  if (error instanceof FavoriteAPIError) {
    if (error.code === "FAVORITE_FOLDER_NOT_FOUND") return "收藏夹不存在或已被删除。";
    if (error.code === "FAVORITE_FOLDER_NAME_CONFLICT") return "已经存在同名收藏夹。";
    if (error.code === "RESOURCE_VERSION_CONFLICT") return "收藏夹已在其他页面修改；已刷新真实版本，请再次确认。";
    return `${error.message}（${error.code}）`;
  }
  return "收藏数据暂时无法加载，请重试。";
}

function pageWithFavorite(page: FavoritePage, gameId: string, favorite: FavoriteReference | null): FavoritePage {
  if (!favorite) return { ...page, items: page.items.filter((item) => item.gameId !== gameId) };
  return { ...page, items: page.items.map((item) => item.gameId === gameId ? { ...item, favorite } : item) };
}

export function FavoriteBrowser({
  initialPage, initialQuery, initialError = "",
}: {
  initialPage: FavoritePage | null; initialQuery: FavoriteQuery; initialError?: string;
}) {
  const { authenticatedFetch } = useAuth();
  const [page, setPage] = useState(initialPage);
  const [query, setQuery] = useState(initialQuery);
  const [search, setSearch] = useState(initialQuery.q);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(initialError);
  const [selecting, setSelecting] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [creating, setCreating] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [folderError, setFolderError] = useState("");
  const [busy, setBusy] = useState(false);
  const [batchPickerAnchor, setBatchPickerAnchor] = useState<HTMLElement | null>(null);
  const [batchFolderIds, setBatchFolderIds] = useState<string[]>([]);
  const [batchCreate, setBatchCreate] = useState(false);
  const [batchUnfavorite, setBatchUnfavorite] = useState(false);
  const [toast, setToast] = useState<ToastState | null>(null);
  const initial = useRef(true);
  const requestSequence = useRef(0);
  const batchAddButton = useRef<HTMLButtonElement>(null);

  const currentFolder = useMemo(() => page?.folders.find((folder) => folder.folderId === query.folderId) ?? null, [page, query.folderId]);
  const currentTitle = query.scope === "ALL" ? "全部收藏" : query.scope === "UNCATEGORIZED" ? "未分类" : currentFolder?.name ?? "收藏夹";
  const currentCount = query.scope === "ALL" ? page?.summary.favoriteCount : query.scope === "UNCATEGORIZED" ? page?.summary.uncategorizedCount : currentFolder?.visibleGameCount;
  const currentDescription = query.scope === "ALL"
    ? `你收藏的所有游戏，共 ${currentCount ?? 0} 款。`
    : query.scope === "UNCATEGORIZED"
      ? `${currentCount ?? 0} 款游戏尚未加入任何自定义收藏夹。`
      : `“${currentTitle}”收藏夹，共 ${currentCount ?? 0} 款。`;

  const refresh = useCallback(async (nextQuery = query) => {
    const sequence = ++requestSequence.current;
    setLoading(true); setError("");
    try {
      const { data } = await loadFavorites(authenticatedFetch, favoriteQueryString(nextQuery));
      if (sequence === requestSequence.current) setPage(data);
    } catch (loadError) {
      const message = errorMessage(loadError);
      if (message && sequence === requestSequence.current) { setPage(null); setError(message); }
    } finally { if (sequence === requestSequence.current) setLoading(false); }
  }, [authenticatedFetch, query]);

  useEffect(() => {
    if (query.q === search) return;
    const timer = window.setTimeout(() => {
      requestSequence.current += 1;
      setQuery((current) => ({ ...current, q: search }));
      setSelecting(false);
      setSelected(new Set());
      setLoading(true);
      setError("");
    }, 150);
    return () => window.clearTimeout(timer);
  }, [query.q, search]);

  useEffect(() => {
    const pathQuery = favoriteQueryString(query).replace(/(?:^|&)limit=50(?:&|$)/, "").replace(/^&|&$/g, "");
    window.history.replaceState(window.history.state, "", `/favorites${pathQuery ? `?${pathQuery}` : ""}`);
    if (initial.current) { initial.current = false; return; }
    const controller = new AbortController();
    const sequence = ++requestSequence.current;
    void loadFavorites(authenticatedFetch, favoriteQueryString(query), controller.signal)
      .then(({ data }) => { if (sequence === requestSequence.current) setPage(data); })
      .catch((loadError: unknown) => {
        const message = errorMessage(loadError);
        if (message && sequence === requestSequence.current) { setPage(null); setError(message); }
      })
      .finally(() => { if (!controller.signal.aborted && sequence === requestSequence.current) setLoading(false); });
    return () => controller.abort();
  }, [authenticatedFetch, query]);

  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(() => setToast(null), 2_000);
    return () => window.clearTimeout(timer);
  }, [toast]);

  function chooseScope(scope: FavoriteQuery["scope"], folderId = "") {
    const next = selectFavoriteScope(query, scope, folderId);
    requestSequence.current += 1;
    setSearch("");
    setSelecting(false); setSelected(new Set()); setLoading(true); setError("");
    setQuery({ ...next, q: "", platformId: "" });
  }

  function organizeUncategorized() {
    chooseScope("UNCATEGORIZED");
    setSelecting(true);
  }

  function updateQuery(update: (current: FavoriteQuery) => FavoriteQuery) {
    requestSequence.current += 1;
    setSelecting(false); setSelected(new Set()); setLoading(true); setError("");
    setQuery(update);
  }

  async function loadMore() {
    if (!page?.nextCursor) return;
    const sequence = ++requestSequence.current;
    setLoading(true);
    try {
      const { data } = await loadFavorites(authenticatedFetch, favoriteQueryString(query, page.nextCursor));
      if (sequence === requestSequence.current) setPage((current) => current ? { ...data, items: [...current.items, ...data.items] } : data);
    } catch (loadError) { if (sequence === requestSequence.current) setError(errorMessage(loadError)); }
    finally { if (sequence === requestSequence.current) setLoading(false); }
  }

  async function createFolder(name: string, gameIds: string[] = []) {
    setBusy(true); setFolderError("");
    try {
      const { data } = await createFavoriteFolder(authenticatedFetch, name, gameIds);
      if (batchCreate) {
        setPage((current) => current ? { ...current, folders: [...current.folders, data] } : current);
        setBatchFolderIds((current) => [...current, data.folderId]);
        setBatchCreate(false);
        setBatchPickerAnchor(batchAddButton.current);
        setToast({ message: `已创建“${data.name}”并加入 ${gameIds.length} 款游戏` });
        await refresh();
      } else {
        setCreating(false);
        chooseScope("FOLDER", data.folderId);
        setToast({ message: `已创建收藏夹“${data.name}”` });
      }
    } catch (createError) { setFolderError(errorMessage(createError)); }
    finally { setBusy(false); }
  }

  async function renameFolder(name: string) {
    if (!currentFolder) return;
    setBusy(true); setFolderError("");
    try {
      await renameFavoriteFolder(authenticatedFetch, currentFolder.folderId, currentFolder.version, name);
      setRenaming(false); setToast({ message: "收藏夹名称已更新" }); await refresh();
    } catch (renameError) { setFolderError(errorMessage(renameError)); await refresh(); }
    finally { setBusy(false); }
  }

  async function deleteFolder() {
    if (!currentFolder) return;
    setBusy(true);
    try {
      await deleteFavoriteFolder(authenticatedFetch, currentFolder.folderId, currentFolder.version);
      setDeleting(false); chooseScope("ALL"); setToast({ message: `已删除收藏夹“${currentFolder.name}”，收藏仍保留` });
    } catch (deleteError) { setFolderError(errorMessage(deleteError)); await refresh(); }
    finally { setBusy(false); }
  }

  async function batchOrganize(addFolderIds: string[], removeFolderIds: string[]) {
    const gameIds = [...selected];
    if (!gameIds.length) return;
    setBusy(true);
    try {
      await organizeFavorites(authenticatedFetch, gameIds, addFolderIds, removeFolderIds);
      setBatchPickerAnchor(null); setSelecting(false); setSelected(new Set());
      setToast({ message: `已整理 ${gameIds.length} 款游戏` }); await refresh();
    } catch (batchError) { setToast({ message: errorMessage(batchError) }); }
    finally { setBusy(false); }
  }

  async function confirmBatchUnfavorite() {
    const gameIds = [...selected];
    setBusy(true);
    try {
      const { data } = await unfavoriteGames(authenticatedFetch, gameIds);
      setBatchUnfavorite(false); setSelecting(false); setSelected(new Set());
      setToast({ message: `已取消收藏 ${data.items.length} 款游戏`, undo: data.items }); await refresh();
    } catch (removeError) { setToast({ message: errorMessage(removeError) }); }
    finally { setBusy(false); }
  }

  async function undo() {
    if (!toast?.undo?.length) return;
    setBusy(true);
    try { await restoreFavorites(authenticatedFetch, toast.undo); setToast({ message: "已恢复收藏" }); await refresh(); }
    catch (restoreError) { setToast({ message: errorMessage(restoreError) }); }
    finally { setBusy(false); }
  }

  return <div className="page-layout favorite-page">
    <PageHeader eyebrow="你的游戏" title="我的收藏" description="快速保存喜欢的游戏，需要时再用收藏夹整理。同一款游戏可以加入多个收藏夹。" actions={<div className="favorite-head-summary"><strong>{page?.summary.favoriteCount ?? 0}</strong> 款收藏 · <strong>{page?.summary.folderCount ?? 0}</strong> 个收藏夹</div>} />
    <div className="favorite-layout">
      <aside className="favorite-rail" aria-label="收藏导航">
        <header><h2>收藏导航</h2><p>全部收藏与自定义收藏夹</p></header>
        <nav>
          <button className={query.scope === "ALL" ? "is-active" : ""} aria-current={query.scope === "ALL" ? "page" : undefined} onClick={() => chooseScope("ALL")}><span aria-hidden="true">♥</span><span>全部收藏</span><strong>{page?.summary.favoriteCount ?? 0}</strong></button>
          <button className={query.scope === "UNCATEGORIZED" ? "is-active" : ""} aria-current={query.scope === "UNCATEGORIZED" ? "page" : undefined} onClick={() => chooseScope("UNCATEGORIZED")}><span aria-hidden="true">○</span><span>未分类</span><strong>{page?.summary.uncategorizedCount ?? 0}</strong></button>
          <p className="favorite-rail-label">收藏夹</p>
          {page?.folders.map((folder) => <button className={query.folderId === folder.folderId ? "is-active" : ""} aria-current={query.folderId === folder.folderId ? "page" : undefined} onClick={() => chooseScope("FOLDER", folder.folderId)} key={folder.folderId}><span aria-hidden="true">▣</span><span>{folder.name}</span><strong>{folder.visibleGameCount}</strong></button>)}
        </nav>
        <button className="favorite-new-folder" type="button" onClick={() => { setFolderError(""); setCreating(true); }}>＋ 新建收藏夹</button>
      </aside>
      <section className="favorite-content" aria-labelledby="favorite-view-title">
        <header className="favorite-view-head"><div><h2 id="favorite-view-title">{currentTitle}</h2><p>{currentDescription}</p></div>{currentFolder ? <button className="button secondary" type="button" onClick={() => { setFolderError(""); setRenaming(true); }}>编辑收藏夹</button> : null}</header>
        <div className="favorite-toolbar" aria-label="收藏筛选">
          <label><span>搜索收藏</span><span className="favorite-search"><AppIcon name="search" /><input type="search" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="输入游戏标题" /></span></label>
          <label><span>排序方式</span><select value={query.sort} onChange={(event) => updateQuery((current) => ({ ...current, sort: event.target.value as FavoriteQuery["sort"] }))}><option value="FAVORITED_DESC">最近收藏</option><option value="RECENTLY_PLAYED_DESC">最近游玩</option><option value="TITLE_ASC">名称 A–Z</option><option value="RELEASE_YEAR_DESC">发行年份</option></select></label>
          <button className="button secondary" type="button" aria-pressed={selecting} onClick={() => { setSelecting((value) => !value); setSelected(new Set()); }}>{selecting ? "完成整理" : "批量整理"}</button>
        </div>
        {page ? <div className="favorite-platforms"><span>游戏平台</span><button className={!query.platformId ? "is-active" : ""} aria-pressed={!query.platformId} onClick={() => updateQuery((current) => ({ ...current, platformId: "" }))}>全部 <strong>{currentCount ?? 0}</strong></button>{page.platforms.map((platform) => <button className={query.platformId === platform.id ? "is-active" : ""} aria-pressed={query.platformId === platform.id} onClick={() => updateQuery((current) => ({ ...current, platformId: platform.id }))} key={platform.id}>{platform.name} <strong>{platform.count}</strong></button>)}<span className="favorite-result-count">当前显示 <strong>{page.totalCount}</strong> 款</span>{query.scope === "ALL" && page.summary.uncategorizedCount > 0 ? <button className="favorite-organize-uncategorized" type="button" onClick={organizeUncategorized}>整理未分类游戏</button> : null}</div> : null}
        {loading && !page ? <div className="favorite-loading" role="status">正在加载收藏…</div> : null}
        {error ? <div className="favorite-error" role="alert"><h3>无法显示收藏</h3><p>{error}</p><button className="button" type="button" onClick={() => void refresh()}>重试</button>{query.scope === "FOLDER" ? <button className="button secondary" type="button" onClick={() => chooseScope("ALL")}>返回全部收藏</button> : null}</div> : null}
        {!error && page && page.summary.favoriteCount === 0 ? <div className="favorite-empty"><h3>还没有收藏游戏</h3><p>从游戏库或游戏详情点击收藏，即可在这里统一整理。</p><Link className="button" href="/library">前往游戏库</Link></div> : null}
        {!error && page && page.summary.favoriteCount > 0 && page.totalCount === 0 ? <div className="favorite-empty"><h3>{query.q || query.platformId ? "没有匹配的收藏" : query.scope === "FOLDER" ? "此收藏夹还没有游戏" : "当前视图没有游戏"}</h3><p>{query.q || query.platformId ? "清除筛选后查看当前视图的全部游戏。" : "游戏仍可从游戏库加入此收藏夹。"}</p>{query.q || query.platformId ? <button className="button secondary" type="button" onClick={() => { setSearch(""); updateQuery((current) => ({ ...current, q: "", platformId: "" })); }}>清除筛选</button> : <Link className="button" href="/library">前往游戏库</Link>}</div> : null}
        {!error && page && page.items.length ? <><FavoriteGrid games={page.items} folders={page.folders} selecting={selecting} selected={selected} busy={busy} onToggle={(gameId) => setSelected((current) => toggleGameSelection(current, gameId))} onFavoriteChange={(gameId, favorite) => { setPage((current) => current ? pageWithFavorite(current, gameId, favorite) : current); void refresh(); }} />{page.nextCursor ? <button className="button secondary favorite-load-more" type="button" disabled={loading} onClick={() => void loadMore()}>{loading ? "加载中…" : "加载更多"}</button> : null}</> : null}
      </section>
    </div>
    {selecting && selected.size ? <div className="favorite-batch" role="status" aria-live="polite"><strong>已选择 {selected.size} 款</strong><button ref={batchAddButton} className="is-primary" type="button" disabled={busy} onClick={(event) => { setBatchFolderIds([]); setBatchPickerAnchor(event.currentTarget); }}>加入收藏夹</button>{query.scope === "FOLDER" && currentFolder ? <button type="button" disabled={busy} onClick={() => void batchOrganize([], [currentFolder.folderId])}>从当前收藏夹移除</button> : null}<button className="is-danger" type="button" disabled={busy} onClick={() => setBatchUnfavorite(true)}>取消收藏</button><button type="button" disabled={busy} onClick={() => { setBatchPickerAnchor(null); setSelecting(false); setSelected(new Set()); }}>取消选择</button></div> : null}
    <FolderNameDialog open={creating} title="新建收藏夹" submitLabel="创建收藏夹" busy={busy} error={folderError} onClose={() => setCreating(false)} onSubmit={(name) => void createFolder(name)} />
    <FolderEditDialog open={renaming} initialName={currentFolder?.name ?? ""} busy={busy} error={folderError} onClose={() => setRenaming(false)} onDelete={() => { setRenaming(false); setDeleting(true); }} onSubmit={(name) => void renameFolder(name)} />
    <ConfirmDialog open={deleting} title={`删除“${currentFolder?.name ?? "收藏夹"}”？`} description="收藏夹将从导航中删除，其中的游戏仍会保留在“全部收藏”中。" confirmLabel="删除收藏夹" cancelLabel="取消" tone="danger" busy={busy} onCancel={() => setDeleting(false)} onConfirm={() => void deleteFolder()}><p>此操作不会删除游戏文件、存档或取消游戏收藏。</p>{folderError ? <p className="favorite-form-error" role="alert">{folderError}</p> : null}</ConfirmDialog>
    <FolderPickerDialog open={Boolean(batchPickerAnchor)} anchor={batchPickerAnchor} title={`将 ${selected.size} 款游戏加入收藏夹`} folders={page?.folders ?? []} selectedFolderIds={batchFolderIds} busy={busy} onClose={() => setBatchPickerAnchor(null)} onCreate={() => { setFolderError(""); setBatchPickerAnchor(null); setBatchCreate(true); }} onSave={(folderIds) => void batchOrganize(folderIds, [])} />
    <FolderNameDialog open={batchCreate} title="新建收藏夹" submitLabel="创建收藏夹" busy={busy} error={folderError} onClose={() => { setBatchCreate(false); setBatchPickerAnchor(batchAddButton.current); }} onSubmit={(name) => void createFolder(name, [...selected])} />
    <ConfirmDialog open={batchUnfavorite} title={`取消收藏 ${selected.size} 款游戏？`} description="这些游戏会同时从所有收藏夹移除；提交后可在两秒内撤销。" confirmLabel="取消收藏" cancelLabel="保留收藏" tone="danger" busy={busy} onCancel={() => setBatchUnfavorite(false)} onConfirm={() => void confirmBatchUnfavorite()} />
    {toast ? <div className="favorite-toast" role="status" aria-live="polite"><span>{toast.message}</span>{toast.undo?.length ? <button type="button" disabled={busy} onClick={() => void undo()}>撤销</button> : null}<button type="button" aria-label="关闭通知" onClick={() => setToast(null)}>×</button></div> : null}
  </div>;
}
