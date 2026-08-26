"use client";

import Image from "next/image";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { replaceWithPlayerDocument } from "@/lib/player-document-navigation";
import {
  fetchImmersiveLibraryGames,
  ImmersiveAPIError,
  launchImmersiveGame,
  setImmersiveFavorite,
  type ImmersiveGame,
  type ImmersiveLibraryGameList,
  type ImmersiveLibraryKind,
} from "./api";
import { ImmersiveChoiceDialog } from "./choice-dialog";
import { mergeGamePage, moveGameIndex, pageGameIndex, shouldPrefetchGamePage } from "./game-list-state";
import { ImmersiveShell } from "./immersive-shell";
import type { NavigationAction } from "./input-model";
import libraryStyles from "./library.module.css";
import { MediaStage } from "./media-stage";
import styles from "./immersive.module.css";

type ViewState = "loading" | "ready" | "empty" | "error" | "unauthorized";
type LaunchState = "idle" | "pending" | "error";
type Folder = ImmersiveLibraryGameList["folders"][number];

type LibraryEntry =
  | Readonly<{ kind: "folder"; folder: Folder }>
  | Readonly<{ kind: "game"; game: ImmersiveGame }>;

function libraryReturnPath(kind: ImmersiveLibraryKind, folderId?: string) {
  const query = new URLSearchParams();
  query.set("destinationId", kind);
  if (folderId) {query.set("folderId", folderId);}
  return `/immersive?${query}`;
}

function gameReturnPath(kind: ImmersiveLibraryKind, gameId: string, folderId?: string, saveStateId?: string) {
  const query = new URLSearchParams({ gameId });
  if (folderId) {query.set("folderId", folderId);}
  if (saveStateId) {query.set("saveStateId", saveStateId);}
  return `/immersive/library/${kind}?${query}`;
}

function replaceLibraryHint(kind: ImmersiveLibraryKind, gameId: string, folderId?: string, saveStateId?: string) {
  window.history.replaceState(window.history.state, "", gameReturnPath(kind, gameId, folderId, saveStateId));
}

function formatSaveTime(value: number) {
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false,
  }).format(value);
}

function LibraryTitleList({ entries, onSelect, selectedIndex, selectedRef }: {
  entries: readonly LibraryEntry[];
  onSelect: (index: number) => void;
  selectedIndex: number;
  selectedRef: React.RefObject<HTMLButtonElement | null>;
}) {
  return <div className={styles.titleList} role="listbox" aria-label="沉浸游戏列表">
    {entries.map((entry, index) => <button
      ref={index === selectedIndex ? selectedRef : undefined}
      className={index === selectedIndex ? styles.selectedGame : ""}
      type="button"
      role="option"
      aria-selected={index === selectedIndex}
      tabIndex={index === selectedIndex ? 0 : -1}
      key={entry.kind === "folder" ? `folder:${entry.folder.folderId}` : `game:${entry.game.gameId}`}
      onClick={() => onSelect(index)}
    >
      <span>{entry.kind === "folder" ? "▣" : String(index + 1).padStart(2, "0")}</span>
      <strong>{entry.kind === "folder" ? entry.folder.name : entry.game.title}</strong>
      {entry.kind === "folder" ? <small>{entry.folder.gameCount} 款游戏</small> : null}
      {entry.kind === "game" && entry.game.favorited ? <small aria-label="已收藏">♥</small> : null}
    </button>)}
  </div>;
}

function FolderDetails({ folder }: { folder: Folder }) {
  return <article className={`${styles.gameDetails} ${libraryStyles.folderDetails}`}>
    <div aria-hidden="true" className={libraryStyles.folderGlyph}>▣</div>
    <div className={styles.descriptionPanel}>
      <p>自定义收藏夹</p>
      <h2>{folder.name}</h2>
      <p className={styles.description}>{folder.gameCount} 款游戏</p>
      <strong>按 A 浏览收藏夹</strong>
    </div>
  </article>;
}

function SaveDetails({ game, saveIndex }: { game: ImmersiveGame; saveIndex: number }) {
  const selectedSave = game.saveStates[saveIndex];
  return <article className={styles.gameDetails}>
    <MediaStage key={game.gameId} game={game} />
    <div className={`${styles.descriptionPanel} ${libraryStyles.savePanel}`}>
      <p>我的存档 · {saveIndex + 1} / {game.saveStates.length}</p>
      <h2>{game.title}</h2>
      {selectedSave ? <div className={libraryStyles.savePreview}>
        <div className={libraryStyles.saveScreenshot}>
          {selectedSave.screenshotUrl ? <Image
              src={selectedSave.screenshotUrl}
              alt={`${selectedSave.name} 存档截图`}
              fill
              sizes="34vw"
              unoptimized
            /> : <span>未提供截图</span>}
        </div>
        <div><strong>{selectedSave.name}</strong><time dateTime={new Date(selectedSave.createdAtMs).toISOString()}>{formatSaveTime(selectedSave.createdAtMs)}</time></div>
      </div> : <p>该游戏暂无可用存档。</p>}
    </div>
  </article>;
}

function GameDetails({ game }: { game: ImmersiveGame }) {
  const metadata = [game.releaseYear, game.developer, game.genre].filter((value) => value !== null && value !== "");
  return <article className={styles.gameDetails}>
    <MediaStage key={game.gameId} game={game} />
    <div className={styles.descriptionPanel}>
      <p>{game.platformInstance.name} · {game.defaultCore.name}</p>
      <h2>{game.title}</h2>
      <div className={styles.gameMetadata}>{metadata.map((value, index) => <span key={`${index}:${String(value)}`}>{value}</span>)}</div>
      <p className={styles.description} tabIndex={0} aria-label={game.description || "暂无游戏简介"}>
        {game.description || "暂无游戏简介"}
      </p>
    </div>
  </article>;
}

function LibraryReadyDetails({ kind, saveIndex, selected }: {
  kind: ImmersiveLibraryKind;
  saveIndex: number;
  selected: LibraryEntry | null;
}) {
  if (selected?.kind === "folder") {return <FolderDetails folder={selected.folder} />;}
  if (!selected) {return null;}
  if (kind === "saves") {return <SaveDetails game={selected.game} saveIndex={saveIndex} />;}
  return <GameDetails game={selected.game} />;
}

function LibraryStateContent({ onRetry, state }: { onRetry: () => void; state: ViewState }) {
  if (state === "loading") {
    return <div className={styles.centerState} role="status"><span className={styles.spinner} />正在读取游戏列表…</div>;
  }
  if (state === "empty") {return <div className={styles.centerState}><h2>这里还没有游戏</h2><p>返回选择其他入口。</p></div>;}
  if (state === "error") {
    return <div className={styles.centerState} role="alert"><h2>无法读取游戏列表</h2><button type="button" onClick={onRetry}>重试</button></div>;
  }
  if (state === "unauthorized") {return <div className={styles.centerState} role="alert"><h2>登录状态已失效</h2></div>;}
  return null;
}

function libraryHelp(kind: ImmersiveLibraryKind, selected: LibraryEntry | null, state: ViewState) {
  if (state !== "ready") {return [{ button: "A", label: "重试" }, { button: "B", label: "返回" }];}
  return [
    { button: "vertical", label: "选择游戏" },
    { button: "horizontal", label: kind === "saves" ? "切换存档" : "快速翻页" },
    { button: "A", label: selected?.kind === "folder" ? "打开收藏夹" : "开始游戏" },
    { button: "Y", label: "收藏" },
    { button: "B", label: "返回" },
  ];
}

function LibraryGameListContent({
  entries,
  favoritePending,
  kind,
  launchState,
  message,
  onAction,
  onRetry,
  onSelect,
  page,
  saveIndex,
  selected,
  selectedIndex,
  selectedRef,
  state,
}: {
  entries: readonly LibraryEntry[];
  favoritePending: boolean;
  kind: ImmersiveLibraryKind;
  launchState: LaunchState;
  message: string;
  onAction: (action: NavigationAction) => void;
  onRetry: () => void;
  onSelect: (index: number) => void;
  page: ImmersiveLibraryGameList | null;
  saveIndex: number;
  selected: LibraryEntry | null;
  selectedIndex: number;
  selectedRef: React.RefObject<HTMLButtonElement | null>;
  state: ViewState;
}) {
  return <ImmersiveShell
    help={libraryHelp(kind, selected, state)}
    inputEpoch={`${state}:${launchState}:${favoritePending}`}
    onAction={onAction}
  >
    <section className={styles.gameListView} aria-labelledby="library-list-title">
      <aside className={styles.gameTitles}>
        <div>
          <p>游戏资料库</p>
          <h1 id="library-list-title">{page?.folder?.name ?? page?.library.name ?? "读取中"}</h1>
          <span>{page?.library.gameCount ?? 0} 款游戏</span>
        </div>
        {state === "ready" ? <LibraryTitleList
          entries={entries}
          selectedIndex={selectedIndex}
          selectedRef={selectedRef}
          onSelect={onSelect}
        /> : null}
      </aside>
      {state === "ready"
        ? <LibraryReadyDetails kind={kind} saveIndex={saveIndex} selected={selected} />
        : <LibraryStateContent state={state} onRetry={onRetry} />}
    </section>
    {message && launchState !== "error"
      ? <p className={libraryStyles.statusMessage} role="status">{message}</p>
      : null}
    {launchState === "pending" ? <div className={styles.launchOverlay} role="status">
      <span className={styles.spinner} /><h2>正在准备运行环境…</h2>
    </div> : null}
    {launchState === "error" ? <ImmersiveChoiceDialog
      title="无法启动游戏"
      description={message}
      selectedId="retry"
      choices={[{ id: "close", label: "返回列表" }, { id: "retry", label: "重试" }]}
      onChoose={(choice) => onAction(choice === "retry" ? "confirm" : "cancel")}
    /> : null}
  </ImmersiveShell>;
}

export function LibraryGameListView({ folderId, initialGameId, initialSaveStateId, kind }: {
  folderId?: string;
  initialGameId?: string;
  initialSaveStateId?: string;
  kind: ImmersiveLibraryKind;
}) {
  const router = useRouter();
  const [state, setState] = useState<ViewState>("loading");
  const [page, setPage] = useState<ImmersiveLibraryGameList | null>(null);
  const [games, setGames] = useState<ImmersiveGame[]>([]);
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [selectedSaveID, setSelectedSaveID] = useState<string | null>(null);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [launchState, setLaunchState] = useState<LaunchState>("idle");
  const [message, setMessage] = useState("");
  const [favoritePending, setFavoritePending] = useState(false);
  const requestRef = useRef<AbortController | null>(null);
  const selectedRef = useRef<HTMLButtonElement>(null);
  const entries = useMemo<LibraryEntry[]>(() => [
    ...(page?.folders ?? []).map((folder) => ({ kind: "folder" as const, folder })),
    ...games.map((game) => ({ kind: "game" as const, game })),
  ], [games, page?.folders]);
  const selected = entries[selectedIndex] ?? null;
  const selectedGame = selected?.kind === "game" ? selected.game : null;
  const saveIndex = selectedGame
    ? Math.max(0, selectedGame.saveStates.findIndex((save) =>
      save.saveStateId === (selectedSaveID ?? initialSaveStateId)))
    : 0;

  const loadInitial = useCallback(() => {
    const controller = new AbortController();
    requestRef.current?.abort();
    requestRef.current = controller;
    void fetchImmersiveLibraryGames(kind, null, folderId, controller.signal).then((result) => {
      setPage(result);
      setGames(result.items);
      setNextCursor(result.nextCursor);
      const gameIndex = result.items.findIndex((game) => game.gameId === initialGameId);
      setSelectedIndex(gameIndex >= 0 ? result.folders.length + gameIndex : 0);
      setState(result.items.length || result.folders.length ? "ready" : "empty");
    }).catch((error: unknown) => {
      if (error instanceof DOMException && error.name === "AbortError") {return;}
      setState(error instanceof ImmersiveAPIError && error.status === 401 ? "unauthorized" : "error");
    }).finally(() => {
      if (requestRef.current === controller) {requestRef.current = null;}
    });
    return () => controller.abort();
  }, [folderId, initialGameId, kind]);

  useEffect(() => {
    queueMicrotask(() => setState("loading"));
    return loadInitial();
  }, [loadInitial]);
  useEffect(() => () => requestRef.current?.abort(), []);

  const retryInitial = useCallback(() => {
    setState("loading");
    loadInitial();
  }, [loadInitial]);

  const loadNext = useCallback(() => {
    if (!nextCursor || requestRef.current) {return;}
    const controller = new AbortController();
    requestRef.current = controller;
    void fetchImmersiveLibraryGames(kind, nextCursor, folderId, controller.signal).then((result) => {
      setGames((current) => mergeGamePage(current, result.items));
      setNextCursor(result.nextCursor);
    }).catch(() => setMessage("后续游戏加载失败，请稍后重试。"))
      .finally(() => {if (requestRef.current === controller) {requestRef.current = null;}});
  }, [folderId, kind, nextCursor]);

  useEffect(() => {
    const selectedGameIndex = selectedIndex - (page?.folders.length ?? 0);
    if (shouldPrefetchGamePage(selectedGameIndex, games.length, nextCursor)) {loadNext();}
  }, [games.length, loadNext, nextCursor, page?.folders.length, selectedIndex]);

  useEffect(() => {
    selectedRef.current?.focus({ preventScroll: true });
    selectedRef.current?.scrollIntoView({ block: "center" });
  }, [selectedGame]);

  useEffect(() => {
    if (!selectedGame) {return;}
    const saveStateId = kind === "saves" ? selectedGame.saveStates[saveIndex]?.saveStateId : undefined;
    replaceLibraryHint(kind, selectedGame.gameId, folderId, saveStateId);
  }, [folderId, kind, saveIndex, selectedGame]);

  const goBack = useCallback(() => {
    if (kind === "favorites" && folderId) {router.push("/immersive/library/favorites"); return;}
    router.push(libraryReturnPath(kind));
  }, [folderId, kind, router]);

  const changeSave = useCallback((direction: "left" | "right") => {
    if (!selectedGame?.saveStates.length) {return;}
    const next = (saveIndex + (direction === "right" ? 1 : -1) + selectedGame.saveStates.length) %
      selectedGame.saveStates.length;
    setSelectedSaveID(selectedGame.saveStates[next].saveStateId);
  }, [saveIndex, selectedGame]);

  const launch = useCallback(() => {
    if (!selectedGame || launchState === "pending") {return;}
    const saveState = kind === "saves" ? selectedGame.saveStates[saveIndex] : null;
    if (kind === "saves" && !saveState) {return;}
    const returnTo = gameReturnPath(kind, selectedGame.gameId, folderId, saveState?.saveStateId);
    setLaunchState("pending");
    setMessage("");
    void launchImmersiveGame(selectedGame.gameId, returnTo, saveState?.saveStateId ?? null)
      .then(replaceWithPlayerDocument)
      .catch((error: unknown) => {
        setMessage(error instanceof Error ? error.message : "当前游戏无法启动");
        setLaunchState("error");
      });
  }, [folderId, kind, launchState, saveIndex, selectedGame]);

  const toggleFavorite = useCallback(() => {
    if (!selectedGame || favoritePending) {return;}
    const next = !selectedGame.favorited;
    setFavoritePending(true);
    setMessage("");
    void setImmersiveFavorite(selectedGame.gameId, next).then(() => {
      if (kind === "favorites" && !next) {
        setGames((current) => current.filter((game) => game.gameId !== selectedGame.gameId));
        setSelectedIndex((current) => Math.max(0, Math.min(current, entries.length - 2)));
      } else {
        setGames((current) => current.map((game) => game.gameId === selectedGame.gameId
          ? { ...game, favorited: next }
          : game));
      }
      setMessage(next ? "已收藏游戏。" : "已取消收藏。");
    }).catch((error: unknown) => setMessage(error instanceof Error ? error.message : "收藏操作失败"))
      .finally(() => setFavoritePending(false));
  }, [entries.length, favoritePending, kind, selectedGame]);

  const handleLaunchAction = useCallback((action: NavigationAction) => {
    if (launchState === "pending") {return true;}
    if (launchState === "error") {
      if (action === "cancel") {setLaunchState("idle"); setMessage("");}
      if (action === "confirm") {setLaunchState("idle"); window.setTimeout(launch, 0);}
      return true;
    }
    return false;
  }, [launch, launchState]);

  const handleViewStateAction = useCallback((action: NavigationAction) => {
    if (state !== "ready") {
      if (state === "error" && action === "confirm") {retryInitial();}
      if (action === "cancel") {goBack();}
      return true;
    }
    return false;
  }, [goBack, retryInitial, state]);

  const handleReadyAction = useCallback((action: NavigationAction) => {
    if (favoritePending) {return;}
    if (action === "up") {setSelectedIndex((value) => moveGameIndex(value, "up", entries.length));}
    if (action === "down") {setSelectedIndex((value) => moveGameIndex(value, "down", entries.length));}
    if (action === "left" || action === "right") {
      if (kind === "saves") {changeSave(action);}
      else {setSelectedIndex((value) => pageGameIndex(value, action, entries.length));}
    }
    if (action === "favorite") {toggleFavorite();}
    if (action === "confirm") {
      if (selected?.kind === "folder") {
        router.push(`/immersive/library/favorites?folderId=${encodeURIComponent(selected.folder.folderId)}`);
      } else {launch();}
    }
    if (action === "cancel") {goBack();}
  }, [changeSave, entries.length, favoritePending, goBack, kind, launch, router, selected, toggleFavorite]);

  const onAction = useCallback((action: NavigationAction) => {
    if (handleLaunchAction(action) || handleViewStateAction(action)) {return;}
    handleReadyAction(action);
  }, [handleLaunchAction, handleReadyAction, handleViewStateAction]);

  return <LibraryGameListContent {...{
    entries,
    favoritePending,
    kind,
    launchState,
    message,
    onAction,
    page,
    saveIndex,
    selected,
    selectedIndex,
    selectedRef,
    state,
    onRetry: retryInitial,
    onSelect: setSelectedIndex,
  }} />;
}
