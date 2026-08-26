"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { replaceWithPlayerDocument } from "@/lib/player-document-navigation";
import { fetchImmersiveGames, ImmersiveAPIError, launchImmersiveGame, setImmersiveFavorite, type ImmersiveGame, type ImmersivePlatform } from "./api";
import { ImmersiveChoiceDialog } from "./choice-dialog";
import { fetchInitialGameList } from "./game-list-loader";
import { initialGameIndex, mergeGamePage, moveGameIndex, pageGameIndex, shouldPrefetchGamePage } from "./game-list-state";
import { ImmersiveShell } from "./immersive-shell";
import type { NavigationAction } from "./input-model";
import libraryStyles from "./library.module.css";
import { MediaStage } from "./media-stage";
import styles from "./immersive.module.css";

type ViewState = "loading" | "ready" | "empty" | "error" | "unauthorized";
type LaunchState = "idle" | "pending" | "error";
type BrowseAction = Extract<NavigationAction, "left" | "right" | "up" | "down">;

function isBrowseAction(action: NavigationAction): action is BrowseAction {
  return action === "left" || action === "right" || action === "up" || action === "down";
}

function gameMeta(game: ImmersiveGame) {
  return [game.releaseYear, game.developer, game.genre].filter((value) => value !== null && value !== "");
}

function replaceGameHint(platformId: string, gameId: string) {
  const path = `/immersive/platforms/${encodeURIComponent(platformId)}?gameId=${encodeURIComponent(gameId)}`;
  window.history.replaceState(window.history.state, "", path);
}

function GameReadyContent({ games, loadNext, onSelect, pageError, platform, retrySelected, selected, selectedIndex, selectedRef, titleCounts }: {
  games: readonly ImmersiveGame[];
  loadNext: () => void;
  onSelect: (index: number) => void;
  pageError: boolean;
  platform: ImmersivePlatform;
  retrySelected: boolean;
  selected: ImmersiveGame;
  selectedIndex: number;
  selectedRef: React.RefObject<HTMLButtonElement | null>;
  titleCounts: ReadonlyMap<string, number>;
}) {
  return <>
    <aside className={styles.gameTitles}>
      <div><p>游戏平台</p><h1 id="game-list-title">{platform.platformName}</h1><span>{platform.gameCount} 款游戏</span></div>
      <div className={styles.titleList} role="listbox" aria-label={`${platform.platformName} 游戏`}>
        {games.map((game, index) => <button
          ref={index === selectedIndex ? selectedRef : undefined}
          className={index === selectedIndex && !retrySelected ? styles.selectedGame : ""}
          type="button"
          role="option"
          aria-selected={index === selectedIndex && !retrySelected}
          tabIndex={index === selectedIndex && !retrySelected ? 0 : -1}
          key={game.gameId}
          onClick={() => onSelect(index)}
        ><span>{String(index + 1).padStart(2, "0")}</span><strong>{game.title}</strong>{game.favorited ? <small aria-label="已收藏">♥</small> : null}{(titleCounts.get(game.title) ?? 0) > 1 ? <small>{game.platformInstance.name}</small> : null}</button>)}
        {pageError ? <button type="button" role="option" aria-selected={retrySelected} tabIndex={retrySelected ? 0 : -1} className={retrySelected ? styles.selectedGame : ""} onClick={loadNext}><span>!</span><strong>加载失败，重试</strong></button> : null}
      </div>
    </aside>
    <article className={styles.gameDetails}>
      <MediaStage key={selected.gameId} game={selected} />
      <div className={styles.descriptionPanel}>
        <p>{selected.platformInstance.name} · {selected.defaultCore.name}</p>
        <h2>{selected.title}</h2>
        <div className={styles.gameMetadata}>{gameMeta(selected).map((value, index) => <span key={`${index}:${String(value)}`}>{value}</span>)}</div>
        <p
          key={selected.gameId}
          className={styles.description}
          data-immersive-description="true"
          tabIndex={0}
          aria-label={selected.description || "暂无游戏简介"}
        >{selected.description || "暂无游戏简介"}</p>
      </div>
    </article>
  </>;
}

function GameStateContent({ goBack, onRetry, onUnauthenticated, state }: {
  goBack: () => void;
  onRetry: () => void;
  onUnauthenticated: () => void;
  state: ViewState;
}) {
  if (state === "loading") {return <div className={styles.centerState} role="status"><span className={styles.spinner} />正在读取游戏列表…</div>;}
  if (state === "error") {return <div className={styles.centerState} role="alert"><h1>无法读取平台游戏</h1><p>请检查服务状态后重试。</p><button type="button" onClick={onRetry}>重试</button></div>;}
  if (state === "empty") {return <div className={styles.centerState}><h1>该平台暂时没有可游玩的游戏</h1><button type="button" onClick={goBack}>返回平台选择</button></div>;}
  if (state === "unauthorized") {return <div className={styles.centerState} role="alert"><h1>登录状态已失效</h1><p>请返回登录后重新进入沉浸模式。</p><button type="button" onClick={onUnauthenticated}>返回登录</button></div>;}
  return null;
}

function handleRetrySelectionAction(
  action: NavigationAction,
  loadNext: () => void,
  setRetrySelected: (selected: boolean) => void,
) {
  if (action === "up" || action === "left" || action === "cancel") {setRetrySelected(false);}
  if (action === "confirm") {loadNext();}
}

export function GameListView({ initialGameId, platformId }: { initialGameId?: string; platformId: string }) {
  const router = useRouter();
  const [state, setState] = useState<ViewState>("loading");
  const [platform, setPlatform] = useState<ImmersivePlatform | null>(null);
  const [games, setGames] = useState<ImmersiveGame[]>([]);
  const [selectedIndex, setSelectedIndex] = useState(-1);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [pageError, setPageError] = useState(false);
  const [retrySelected, setRetrySelected] = useState(false);
  const [launchState, setLaunchState] = useState<LaunchState>("idle");
  const [launchMessage, setLaunchMessage] = useState("");
  const [favoritePending, setFavoritePending] = useState(false);
  const requestGeneration = useRef(0);
  const nextRequest = useRef<AbortController | null>(null);
  const pendingBrowseAction = useRef<BrowseAction | null>(null);
  const selectedRef = useRef<HTMLButtonElement>(null);
  const selected = selectedIndex >= 0 ? games[selectedIndex] : null;

  const requestInitial = useCallback(() => {
    const generation = requestGeneration.current + 1;
    requestGeneration.current = generation;
    const controller = new AbortController();
    nextRequest.current?.abort();
    void fetchInitialGameList(platformId, initialGameId, controller.signal).then((result) => {
      if (requestGeneration.current !== generation) {return;}
      setPlatform(result.platform);
      setGames(result.items);
      setNextCursor(result.nextCursor);
      setSelectedIndex(initialGameIndex(result.items, initialGameId));
      setState(result.items.length ? "ready" : "empty");
    }).catch((error: unknown) => {
      if (error instanceof DOMException && error.name === "AbortError") {return;}
      if (error instanceof ImmersiveAPIError && error.status === 404) {setState("empty"); return;}
      setState(error instanceof ImmersiveAPIError && error.status === 401 ? "unauthorized" : "error");
    }).finally(() => {
      if (nextRequest.current === controller) {nextRequest.current = null;}
    });
    return () => controller.abort();
  }, [initialGameId, platformId]);

  useEffect(() => requestInitial(), [requestInitial]);
  useEffect(() => () => nextRequest.current?.abort(), []);

  const retryInitial = useCallback(() => {
    setState("loading");
    setPageError(false);
    requestInitial();
  }, [requestInitial]);

  const loadNext = useCallback(() => {
    if (!nextCursor || nextRequest.current) {return;}
    const cursor = nextCursor;
    const generation = requestGeneration.current;
    const controller = new AbortController();
    nextRequest.current = controller;
    setPageError(false);
    setRetrySelected(false);
    void fetchImmersiveGames(platformId, cursor, controller.signal).then((result) => {
      if (requestGeneration.current !== generation) {return;}
      setGames((current) => mergeGamePage(current, result.items));
      setNextCursor(result.nextCursor);
    }).catch((error: unknown) => {
      if (!(error instanceof DOMException && error.name === "AbortError")) {setPageError(true);}
    }).finally(() => {
      if (nextRequest.current === controller) {nextRequest.current = null;}
    });
  }, [nextCursor, platformId]);

  useEffect(() => {
    if (shouldPrefetchGamePage(selectedIndex, games.length, nextCursor)) {loadNext();}
  }, [games.length, loadNext, nextCursor, selectedIndex]);

  useEffect(() => {
    if (!selected) {return;}
    replaceGameHint(platformId, selected.gameId);
    selectedRef.current?.focus({ preventScroll: true });
    selectedRef.current?.scrollIntoView({ block: "center", behavior: "auto" });
  }, [platformId, selected]);

  useEffect(() => {
    if (state !== "ready" || selectedIndex < 0 || !pendingBrowseAction.current) {return;}
    const action = pendingBrowseAction.current;
    pendingBrowseAction.current = null;
    setSelectedIndex((value) => action === "up" || action === "down"
      ? moveGameIndex(value, action, games.length)
      : pageGameIndex(value, action, games.length));
  }, [games.length, selectedIndex, state]);

  const goBack = useCallback(() => router.push(`/immersive?platformId=${encodeURIComponent(platformId)}`), [platformId, router]);

  const launch = useCallback(() => {
    if (!selected || launchState === "pending") {return;}
    const returnTo = `/immersive/platforms/${platformId}?gameId=${selected.gameId}`;
    setLaunchState("pending");
    setLaunchMessage("");
    void launchImmersiveGame(selected.gameId, returnTo).then(replaceWithPlayerDocument).catch((error: unknown) => {
      setLaunchMessage(error instanceof Error ? error.message : "当前游戏无法启动");
      setLaunchState("error");
    });
  }, [launchState, platformId, selected]);

  const retryLaunch = useCallback(() => {
    setLaunchState("idle");
    window.setTimeout(launch, 0);
  }, [launch]);

  const toggleFavorite = useCallback(() => {
    if (!selected || favoritePending) {return;}
    const next = !selected.favorited;
    setFavoritePending(true);
    setLaunchMessage("");
    void setImmersiveFavorite(selected.gameId, next).then(() => {
      setGames((current) => current.map((game) => game.gameId === selected.gameId
        ? { ...game, favorited: next }
        : game));
      setLaunchMessage(next ? "已收藏游戏。" : "已取消收藏。");
    }).catch((error: unknown) => setLaunchMessage(error instanceof Error ? error.message : "收藏操作失败"))
      .finally(() => setFavoritePending(false));
  }, [favoritePending, selected]);

  const handleLaunchAction = useCallback((action: NavigationAction) => {
    if (launchState === "pending") {return true;}
    if (launchState === "error") {
      if (action === "cancel") {setLaunchState("idle"); setLaunchMessage("");}
      if (action === "confirm") {retryLaunch();}
      return true;
    }
    return false;
  }, [launchState, retryLaunch]);

  const handleViewStateAction = useCallback((action: NavigationAction) => {
    if (state === "loading" && isBrowseAction(action)) {
      pendingBrowseAction.current = action;
      return true;
    }
    if (state === "error") {
      if (action === "confirm") {retryInitial();}
      if (action === "cancel") {goBack();}
      return true;
    }
    if (state === "empty" && (action === "confirm" || action === "cancel")) {goBack();}
    if (state === "unauthorized" && (action === "confirm" || action === "cancel")) {router.replace("/login");}
    return state !== "ready";
  }, [goBack, retryInitial, router, state]);

  const handleReadyAction = useCallback((action: NavigationAction) => {
    if (favoritePending) {return;}
    if (retrySelected) {
      handleRetrySelectionAction(action, loadNext, setRetrySelected);
      return;
    }
    if (action === "up") {setSelectedIndex((value) => moveGameIndex(value, "up", games.length));}
    if (action === "down") {
      if (selectedIndex === games.length - 1 && pageError) {setRetrySelected(true);}
      else {setSelectedIndex((value) => moveGameIndex(value, "down", games.length));}
    }
    if (action === "left" || action === "right") {
      setSelectedIndex((value) => pageGameIndex(value, action, games.length));
    }
    if (action === "confirm") {launch();}
    if (action === "favorite") {toggleFavorite();}
    if (action === "cancel") {goBack();}
  }, [favoritePending, games.length, goBack, launch, loadNext, pageError, retrySelected, selectedIndex, toggleFavorite]);

  const onAction = useCallback((action: NavigationAction) => {
    if (handleLaunchAction(action) || handleViewStateAction(action)) {return;}
    handleReadyAction(action);
  }, [handleLaunchAction, handleReadyAction, handleViewStateAction]);

  const titleCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const game of games) {counts.set(game.title, (counts.get(game.title) ?? 0) + 1);}
    return counts;
  }, [games]);

  const help = state === "ready"
    ? [
      { button: "vertical", label: "选择游戏" },
      { button: "horizontal", label: "快速翻页" },
      { button: "A", label: retrySelected ? "重试加载" : "开始游戏" },
      { button: "Y", label: "收藏" },
      { button: "B", label: "返回平台" },
    ]
    : [{ button: "A", label: state === "error" ? "重试" : "返回" }, { button: "B", label: "返回" }];
  return <ImmersiveShell help={help} inputEpoch={`${state}:${launchState}:${retrySelected}`} onAction={onAction}>
    <section className={styles.gameListView} aria-labelledby="game-list-title">
      {state === "ready" && selected && platform ? <GameReadyContent {...{
        games, loadNext, pageError, platform, retrySelected, selected, selectedIndex, selectedRef, titleCounts,
        onSelect: (index: number) => {setSelectedIndex(index); setRetrySelected(false);},
      }} /> : <GameStateContent state={state} goBack={goBack} onRetry={retryInitial} onUnauthenticated={() => router.replace("/login")} />}
    </section>
    {launchState === "pending" ? <div className={styles.launchOverlay} role="status"><span className={styles.spinner} /><h2>正在准备运行环境…</h2><p>检查游戏内容、核心与运行依赖</p></div> : null}
    {launchMessage && launchState === "idle" ? <p className={libraryStyles.statusMessage} role="status">{launchMessage}</p> : null}
    {launchState === "error" ? <ImmersiveChoiceDialog title="无法启动游戏" description={launchMessage} selectedId="retry" choices={[{ id: "close", label: "返回列表" }, { id: "retry", label: "重试" }]} onChoose={(choice) => {if (choice === "retry") {retryLaunch();} else {setLaunchState("idle");}}} /> : null}
  </ImmersiveShell>;
}
