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
  if (error instanceof DOMException && error.name === "AbortError") {return "";}
  if (error instanceof FavoriteAPIError) {
    if (error.code === "FAVORITE_FOLDER_NOT_FOUND") {return "收藏夹不存在或已被删除。";}
    if (error.code === "FAVORITE_FOLDER_NAME_CONFLICT") {return "已经存在同名收藏夹。";}
    if (error.code === "RESOURCE_VERSION_CONFLICT") {return "收藏夹已在其他页面修改；已刷新真实版本，请再次确认。";}
    return `${error.message}（${error.code}）`;
  }
  return "收藏数据暂时无法加载，请重试。";
}

function pageWithFavorite(page: FavoritePage, gameId: string, favorite: FavoriteReference | null): FavoritePage {
  if (!favorite) {return { ...page, items: page.items.filter((item) => item.gameId !== gameId) };}
  return { ...page, items: page.items.map((item) => item.gameId === gameId ? { ...item, favorite } : item) };
}

type FavoriteFolder = FavoritePage["folders"][number];

function FavoriteNavigation({ onChooseScope, onCreate, page, query }: {
  onChooseScope: (scope: FavoriteQuery["scope"], folderId?: string) => void;
  onCreate: () => void;
  page: FavoritePage | null;
  query: FavoriteQuery;
}) {
  return <aside className="favorite-rail" aria-label="收藏导航">
    <header><h2>收藏导航</h2><p>全部收藏与自定义收藏夹</p></header>
    <nav>
      <button className={query.scope === "ALL" ? "is-active" : ""} aria-current={query.scope === "ALL" ? "page" : undefined} onClick={() => onChooseScope("ALL")}><span aria-hidden="true">♥</span><span>全部收藏</span><strong>{page?.summary.favoriteCount ?? 0}</strong></button>
      <button className={query.scope === "UNCATEGORIZED" ? "is-active" : ""} aria-current={query.scope === "UNCATEGORIZED" ? "page" : undefined} onClick={() => onChooseScope("UNCATEGORIZED")}><span aria-hidden="true">○</span><span>未分类</span><strong>{page?.summary.uncategorizedCount ?? 0}</strong></button>
      <p className="favorite-rail-label">收藏夹</p>
      {page?.folders.map((folder) => <button className={query.folderId === folder.folderId ? "is-active" : ""} aria-current={query.folderId === folder.folderId ? "page" : undefined} onClick={() => onChooseScope("FOLDER", folder.folderId)} key={folder.folderId}><span aria-hidden="true">▣</span><span>{folder.name}</span><strong>{folder.visibleGameCount}</strong></button>)}
    </nav>
    <button className="favorite-new-folder" type="button" onClick={onCreate}>＋ 新建收藏夹</button>
  </aside>;
}

function FavoriteToolbar({ currentCount, onOrganizeUncategorized, onSearch, onToggleSelecting, onUpdateQuery, page, query, search, selecting }: {
  currentCount: number | undefined;
  onOrganizeUncategorized: () => void;
  onSearch: (value: string) => void;
  onToggleSelecting: () => void;
  onUpdateQuery: (update: (current: FavoriteQuery) => FavoriteQuery) => void;
  page: FavoritePage | null;
  query: FavoriteQuery;
  search: string;
  selecting: boolean;
}) {
  return <>
    <div className="favorite-toolbar" aria-label="收藏筛选">
      <label><span>搜索收藏</span><span className="favorite-search"><AppIcon name="search" /><input type="search" value={search} onChange={(event) => onSearch(event.target.value)} placeholder="输入游戏标题" /></span></label>
      <label><span>排序方式</span><select value={query.sort} onChange={(event) => onUpdateQuery((current) => ({ ...current, sort: event.target.value as FavoriteQuery["sort"] }))}><option value="FAVORITED_DESC">最近收藏</option><option value="RECENTLY_PLAYED_DESC">最近游玩</option><option value="TITLE_ASC">名称 A–Z</option><option value="RELEASE_YEAR_DESC">发行年份</option></select></label>
      <button className="button secondary" type="button" aria-pressed={selecting} onClick={onToggleSelecting}>{selecting ? "完成整理" : "批量整理"}</button>
    </div>
    {page ? <div className="favorite-platforms"><span>游戏平台</span><button className={!query.platformId ? "is-active" : ""} aria-pressed={!query.platformId} onClick={() => onUpdateQuery((current) => ({ ...current, platformId: "" }))}>全部 <strong>{currentCount ?? 0}</strong></button>{page.platforms.map((platform) => <button className={query.platformId === platform.id ? "is-active" : ""} aria-pressed={query.platformId === platform.id} onClick={() => onUpdateQuery((current) => ({ ...current, platformId: platform.id }))} key={platform.id}>{platform.name} <strong>{platform.count}</strong></button>)}<span className="favorite-result-count">当前显示 <strong>{page.totalCount}</strong> 款</span>{query.scope === "ALL" && page.summary.uncategorizedCount > 0 ? <button className="favorite-organize-uncategorized" type="button" onClick={onOrganizeUncategorized}>整理未分类游戏</button> : null}</div> : null}
  </>;
}

function FavoriteContentState({ error, loading, onChooseAll, onClear, onRefresh, page, query }: {
  error: string;
  loading: boolean;
  onChooseAll: () => void;
  onClear: () => void;
  onRefresh: () => void;
  page: FavoritePage | null;
  query: FavoriteQuery;
}) {
  if (loading && !page) {return <div className="favorite-loading" role="status">正在加载收藏…</div>;}
  if (error) {return <div className="favorite-error" role="alert"><h3>无法显示收藏</h3><p>{error}</p><button className="button" type="button" onClick={onRefresh}>重试</button>{query.scope === "FOLDER" ? <button className="button secondary" type="button" onClick={onChooseAll}>返回全部收藏</button> : null}</div>;}
  if (page?.summary.favoriteCount === 0) {return <div className="favorite-empty"><h3>还没有收藏游戏</h3><p>从游戏库或游戏详情点击收藏，即可在这里统一整理。</p><Link className="button" href="/library">前往游戏库</Link></div>;}
  if (page && page.summary.favoriteCount > 0 && page.totalCount === 0) {
    const filtered = Boolean(query.q || query.platformId);
    const title = filtered ? "没有匹配的收藏" : query.scope === "FOLDER" ? "此收藏夹还没有游戏" : "当前视图没有游戏";
    return <div className="favorite-empty"><h3>{title}</h3><p>{filtered ? "清除筛选后查看当前视图的全部游戏。" : "游戏仍可从游戏库加入此收藏夹。"}</p>{filtered ? <button className="button secondary" type="button" onClick={onClear}>清除筛选</button> : <Link className="button" href="/library">前往游戏库</Link>}</div>;
  }
  return null;
}

function FavoriteGames({ busy, loading, onFavoriteChange, onLoadMore, onToggle, page, selected, selecting }: {
  busy: boolean;
  loading: boolean;
  onFavoriteChange: (gameId: string, favorite: FavoriteReference | null) => void;
  onLoadMore: () => void;
  onToggle: (gameId: string) => void;
  page: FavoritePage | null;
  selected: Set<string>;
  selecting: boolean;
}) {
  if (!page?.items.length) {return null;}
  return <><FavoriteGrid games={page.items} folders={page.folders} selecting={selecting} selected={selected} busy={busy} onToggle={onToggle} onFavoriteChange={onFavoriteChange} />{page.nextCursor ? <button className="button secondary favorite-load-more" type="button" disabled={loading} onClick={onLoadMore}>{loading ? "加载中…" : "加载更多"}</button> : null}</>;
}

function FavoriteBatchBar({ busy, currentFolder, onAdd, onCancel, onRemove, onUnfavorite, selected, selecting }: {
  busy: boolean;
  currentFolder: FavoriteFolder | null;
  onAdd: (element: HTMLButtonElement) => void;
  onCancel: () => void;
  onRemove: (folderId: string) => void;
  onUnfavorite: () => void;
  selected: Set<string>;
  selecting: boolean;
}) {
  if (!selecting || !selected.size) {return null;}
  return <div className="favorite-batch" role="status" aria-live="polite"><strong>已选择 {selected.size} 款</strong><button className="is-primary" type="button" disabled={busy} onClick={(event) => onAdd(event.currentTarget)}>加入收藏夹</button>{currentFolder ? <button type="button" disabled={busy} onClick={() => onRemove(currentFolder.folderId)}>从当前收藏夹移除</button> : null}<button className="is-danger" type="button" disabled={busy} onClick={onUnfavorite}>取消收藏</button><button type="button" disabled={busy} onClick={onCancel}>取消选择</button></div>;
}

function favoriteViewMetadata(page: FavoritePage | null, query: FavoriteQuery, folder: FavoriteFolder | null) {
  if (query.scope === "ALL") {
    const count = page?.summary.favoriteCount;
    return { count, description: `你收藏的所有游戏，共 ${count ?? 0} 款。`, title: "全部收藏" };
  }
  if (query.scope === "UNCATEGORIZED") {
    const count = page?.summary.uncategorizedCount;
    return { count, description: `${count ?? 0} 款游戏尚未加入任何自定义收藏夹。`, title: "未分类" };
  }
  const count = folder?.visibleGameCount;
  const title = folder?.name ?? "收藏夹";
  return { count, description: `“${title}”收藏夹，共 ${count ?? 0} 款。`, title };
}

type FavoriteBrowserViewProps = {
  busy: boolean;
  currentFolder: FavoriteFolder | null;
  error: string;
  loading: boolean;
  metadata: ReturnType<typeof favoriteViewMetadata>;
  onAddBatch: (element: HTMLButtonElement) => void;
  onCancelBatch: () => void;
  onChooseScope: (scope: FavoriteQuery["scope"], folderId?: string) => void;
  onClear: () => void;
  onCreateFolder: () => void;
  onEditFolder: () => void;
  onFavoriteChange: (gameId: string, favorite: FavoriteReference | null) => void;
  onLoadMore: () => void;
  onOrganizeUncategorized: () => void;
  onRefresh: () => void;
  onRemoveBatch: (folderId: string) => void;
  onSearch: (value: string) => void;
  onToggleSelecting: () => void;
  onToggleSelection: (gameId: string) => void;
  onUnfavoriteBatch: () => void;
  onUpdateQuery: (update: (current: FavoriteQuery) => FavoriteQuery) => void;
  page: FavoritePage | null;
  query: FavoriteQuery;
  search: string;
  selected: Set<string>;
  selecting: boolean;
};

function FavoriteBrowserView(props: FavoriteBrowserViewProps) {
  return <>
    <PageHeader eyebrow="你的游戏" title="我的收藏" description="快速保存喜欢的游戏，需要时再用收藏夹整理。同一款游戏可以加入多个收藏夹。" actions={<div className="favorite-head-summary"><strong>{props.page?.summary.favoriteCount ?? 0}</strong> 款收藏 · <strong>{props.page?.summary.folderCount ?? 0}</strong> 个收藏夹</div>} />
    <div className="favorite-layout">
      <FavoriteNavigation onChooseScope={props.onChooseScope} onCreate={props.onCreateFolder} page={props.page} query={props.query} />
      <section className="favorite-content" aria-labelledby="favorite-view-title">
        <header className="favorite-view-head"><div><h2 id="favorite-view-title">{props.metadata.title}</h2><p>{props.metadata.description}</p></div>{props.currentFolder ? <button className="button secondary" type="button" onClick={props.onEditFolder}>编辑收藏夹</button> : null}</header>
        <FavoriteToolbar currentCount={props.metadata.count} onOrganizeUncategorized={props.onOrganizeUncategorized} onSearch={props.onSearch} onToggleSelecting={props.onToggleSelecting} onUpdateQuery={props.onUpdateQuery} page={props.page} query={props.query} search={props.search} selecting={props.selecting} />
        <FavoriteContentState error={props.error} loading={props.loading} onChooseAll={() => props.onChooseScope("ALL")} onClear={props.onClear} onRefresh={props.onRefresh} page={props.page} query={props.query} />
        {!props.error ? <FavoriteGames busy={props.busy} loading={props.loading} onFavoriteChange={props.onFavoriteChange} onLoadMore={props.onLoadMore} onToggle={props.onToggleSelection} page={props.page} selected={props.selected} selecting={props.selecting} /> : null}
      </section>
    </div>
    <FavoriteBatchBar busy={props.busy} currentFolder={props.query.scope === "FOLDER" ? props.currentFolder : null} onAdd={props.onAddBatch} onCancel={props.onCancelBatch} onRemove={props.onRemoveBatch} onUnfavorite={props.onUnfavoriteBatch} selected={props.selected} selecting={props.selecting} />
  </>;
}

type FavoriteDialogsProps = {
  batchCreate: boolean;
  batchFolderIds: string[];
  batchPickerAnchor: HTMLElement | null;
  batchUnfavorite: boolean;
  busy: boolean;
  creating: boolean;
  currentFolder: FavoriteFolder | null;
  deleting: boolean;
  folderError: string;
  onBatchCreateClose: () => void;
  onBatchCreateFolder: () => void;
  onBatchCreateSubmit: (name: string) => void;
  onBatchPickerClose: () => void;
  onBatchSave: (folderIds: string[]) => void;
  onBatchUnfavoriteCancel: () => void;
  onBatchUnfavoriteConfirm: () => void;
  onCreateClose: () => void;
  onCreateSubmit: (name: string) => void;
  onDeleteCancel: () => void;
  onDeleteConfirm: () => void;
  onEditClose: () => void;
  onEditDelete: () => void;
  onEditSubmit: (name: string) => void;
  onToastClose: () => void;
  onUndo: () => void;
  page: FavoritePage | null;
  renaming: boolean;
  selected: Set<string>;
  toast: ToastState | null;
};

function FavoriteDialogs(props: FavoriteDialogsProps) {
  return <>
    <FolderNameDialog open={props.creating} title="新建收藏夹" submitLabel="创建收藏夹" busy={props.busy} error={props.folderError} onClose={props.onCreateClose} onSubmit={props.onCreateSubmit} />
    <FolderEditDialog open={props.renaming} initialName={props.currentFolder?.name ?? ""} busy={props.busy} error={props.folderError} onClose={props.onEditClose} onDelete={props.onEditDelete} onSubmit={props.onEditSubmit} />
    <ConfirmDialog open={props.deleting} title={`删除“${props.currentFolder?.name ?? "收藏夹"}”？`} description="收藏夹将从导航中删除，其中的游戏仍会保留在“全部收藏”中。" confirmLabel="删除收藏夹" cancelLabel="取消" tone="danger" busy={props.busy} onCancel={props.onDeleteCancel} onConfirm={props.onDeleteConfirm}><p>此操作不会删除游戏文件、存档或取消游戏收藏。</p>{props.folderError ? <p className="favorite-form-error" role="alert">{props.folderError}</p> : null}</ConfirmDialog>
    <FolderPickerDialog open={Boolean(props.batchPickerAnchor)} anchor={props.batchPickerAnchor} title={`将 ${props.selected.size} 款游戏加入收藏夹`} folders={props.page?.folders ?? []} selectedFolderIds={props.batchFolderIds} busy={props.busy} onClose={props.onBatchPickerClose} onCreate={props.onBatchCreateFolder} onSave={props.onBatchSave} />
    <FolderNameDialog open={props.batchCreate} title="新建收藏夹" submitLabel="创建收藏夹" busy={props.busy} error={props.folderError} onClose={props.onBatchCreateClose} onSubmit={props.onBatchCreateSubmit} />
    <ConfirmDialog open={props.batchUnfavorite} title={`取消收藏 ${props.selected.size} 款游戏？`} description="这些游戏会同时从所有收藏夹移除；提交后可在两秒内撤销。" confirmLabel="取消收藏" cancelLabel="保留收藏" tone="danger" busy={props.busy} onCancel={props.onBatchUnfavoriteCancel} onConfirm={props.onBatchUnfavoriteConfirm} />
    {props.toast ? <div className="favorite-toast" role="status" aria-live="polite"><span>{props.toast.message}</span>{props.toast.undo?.length ? <button type="button" disabled={props.busy} onClick={props.onUndo}>撤销</button> : null}<button type="button" aria-label="关闭通知" onClick={props.onToastClose}>×</button></div> : null}
  </>;
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
  const metadata = favoriteViewMetadata(page, query, currentFolder);

  const refresh = useCallback(async (nextQuery = query) => {
    const sequence = ++requestSequence.current;
    setLoading(true); setError("");
    try {
      const { data } = await loadFavorites(authenticatedFetch, favoriteQueryString(nextQuery));
      if (sequence === requestSequence.current) {setPage(data);}
    } catch (loadError) {
      const message = errorMessage(loadError);
      if (message && sequence === requestSequence.current) { setPage(null); setError(message); }
    } finally { if (sequence === requestSequence.current) {setLoading(false);} }
  }, [authenticatedFetch, query]);

  useEffect(() => {
    if (query.q === search) {return;}
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
      .then(({ data }) => { if (sequence === requestSequence.current) {setPage(data);} })
      .catch((loadError: unknown) => {
        const message = errorMessage(loadError);
        if (message && sequence === requestSequence.current) { setPage(null); setError(message); }
      })
      .finally(() => { if (!controller.signal.aborted && sequence === requestSequence.current) {setLoading(false);} });
    return () => controller.abort();
  }, [authenticatedFetch, query]);

  useEffect(() => {
    if (!toast) {return;}
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
    if (!page?.nextCursor) {return;}
    const sequence = ++requestSequence.current;
    setLoading(true);
    try {
      const { data } = await loadFavorites(authenticatedFetch, favoriteQueryString(query, page.nextCursor));
      if (sequence === requestSequence.current) {setPage((current) => current ? { ...data, items: [...current.items, ...data.items] } : data);}
    } catch (loadError) { if (sequence === requestSequence.current) {setError(errorMessage(loadError));} }
    finally { if (sequence === requestSequence.current) {setLoading(false);} }
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
    if (!currentFolder) {return;}
    setBusy(true); setFolderError("");
    try {
      await renameFavoriteFolder(authenticatedFetch, currentFolder.folderId, currentFolder.version, name);
      setRenaming(false); setToast({ message: "收藏夹名称已更新" }); await refresh();
    } catch (renameError) { setFolderError(errorMessage(renameError)); await refresh(); }
    finally { setBusy(false); }
  }

  async function deleteFolder() {
    if (!currentFolder) {return;}
    setBusy(true);
    try {
      await deleteFavoriteFolder(authenticatedFetch, currentFolder.folderId, currentFolder.version);
      setDeleting(false); chooseScope("ALL"); setToast({ message: `已删除收藏夹“${currentFolder.name}”，收藏仍保留` });
    } catch (deleteError) { setFolderError(errorMessage(deleteError)); await refresh(); }
    finally { setBusy(false); }
  }

  async function batchOrganize(addFolderIds: string[], removeFolderIds: string[]) {
    const gameIds = [...selected];
    if (!gameIds.length) {return;}
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
    if (!toast?.undo?.length) {return;}
    setBusy(true);
    try { await restoreFavorites(authenticatedFetch, toast.undo); setToast({ message: "已恢复收藏" }); await refresh(); }
    catch (restoreError) { setToast({ message: errorMessage(restoreError) }); }
    finally { setBusy(false); }
  }

  return <div className="page-layout favorite-page">
    <FavoriteBrowserView
      busy={busy} currentFolder={currentFolder} error={error} loading={loading} metadata={metadata}
      onAddBatch={(element) => { batchAddButton.current = element; setBatchFolderIds([]); setBatchPickerAnchor(element); }}
      onCancelBatch={() => { setBatchPickerAnchor(null); setSelecting(false); setSelected(new Set()); }}
      onChooseScope={chooseScope}
      onClear={() => { setSearch(""); updateQuery((current) => ({ ...current, q: "", platformId: "" })); }}
      onCreateFolder={() => { setFolderError(""); setCreating(true); }}
      onEditFolder={() => { setFolderError(""); setRenaming(true); }}
      onFavoriteChange={(gameId, favorite) => { setPage((current) => current ? pageWithFavorite(current, gameId, favorite) : current); void refresh(); }}
      onLoadMore={() => void loadMore()} onOrganizeUncategorized={organizeUncategorized} onRefresh={() => void refresh()}
      onRemoveBatch={(folderId) => void batchOrganize([], [folderId])} onSearch={setSearch}
      onToggleSelecting={() => { setSelecting((value) => !value); setSelected(new Set()); }}
      onToggleSelection={(gameId) => setSelected((current) => toggleGameSelection(current, gameId))}
      onUnfavoriteBatch={() => setBatchUnfavorite(true)} onUpdateQuery={updateQuery}
      page={page} query={query} search={search} selected={selected} selecting={selecting}
    />
    <FavoriteDialogs
      batchCreate={batchCreate} batchFolderIds={batchFolderIds} batchPickerAnchor={batchPickerAnchor} batchUnfavorite={batchUnfavorite}
      busy={busy} creating={creating} currentFolder={currentFolder} deleting={deleting} folderError={folderError}
      onBatchCreateClose={() => { setBatchCreate(false); setBatchPickerAnchor(batchAddButton.current); }}
      onBatchCreateFolder={() => { setFolderError(""); setBatchPickerAnchor(null); setBatchCreate(true); }}
      onBatchCreateSubmit={(name) => void createFolder(name, [...selected])}
      onBatchPickerClose={() => setBatchPickerAnchor(null)} onBatchSave={(folderIds) => void batchOrganize(folderIds, [])}
      onBatchUnfavoriteCancel={() => setBatchUnfavorite(false)} onBatchUnfavoriteConfirm={() => void confirmBatchUnfavorite()}
      onCreateClose={() => setCreating(false)} onCreateSubmit={(name) => void createFolder(name)}
      onDeleteCancel={() => setDeleting(false)} onDeleteConfirm={() => void deleteFolder()}
      onEditClose={() => setRenaming(false)} onEditDelete={() => { setRenaming(false); setDeleting(true); }} onEditSubmit={(name) => void renameFolder(name)}
      onToastClose={() => setToast(null)} onUndo={() => void undo()} page={page} renaming={renaming} selected={selected} toast={toast}
    />
  </div>;
}
