"use client";
import Link from "next/link";
import { AppIcon } from "@/components/app-icon";
import { useCallback, useEffect, useState } from "react";
import { api, result } from "@/lib/api/client";
import { useResource } from "@/lib/use-resource";
import { ResourceState } from "@/components/resource-state";
import {
  loadDirectories,
  loadGames,
  toggleFavorite,
} from "@/features/library/api";
import { useImmersiveNavigation } from "./use-navigation";
import type { NavigationAction } from "./input-model";
import { PlatformCarousel, ImmersiveGames } from "./immersive-content";
import type { ImmersiveEntry } from "./immersive-content";
import {
  getImmersiveAudioPreferences,
  saveImmersiveAudioPreferences,
} from "./immersive-audio-preferences";
import styles from "./immersive.module.css";
import menuStyles from "./system-menu.module.css";
type View = "platforms" | "games" | "favorites" | "saves" | "recent";
export function ImmersiveShell() {
  const directories = useResource(loadDirectories);
  const [view, setView] = useState<View>("platforms");
  const [selected, setSelected] = useState(0);
  const [directory, setDirectory] = useState(0);
  const [offset, setOffset] = useState(0);
  const [menu, setMenu] = useState(false);
  const [menuIndex, setMenuIndex] = useState(0);
  const [folder, setFolder] = useState("");
  const [error, setError] = useState("");
  const folders = useResource(loadFolders);
  const current = directories.data?.items[directory];
  const loader = useCallback(
    () => loadEntries(view, current?.id, folder, offset),
    [view, current?.id, folder, offset],
  );
  const entries = useResource(loader);
  function changeView(next: View) {
    setView(next);
    setSelected(0);
    setOffset(0);
    setMenu(false);
  }
  async function favorite() {
    const game = entries.data?.items[selected]?.game;
    if (!game) {
      return;
    }
    try {
      await toggleFavorite(game.id, !game.favorite);
      entries.reload();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "收藏失败。");
    }
  }
  function launch() {
    document
      .querySelector<HTMLButtonElement>(
        `.${styles.gameDetails} .home-launch-control button`,
      )
      ?.click();
  }
  function navigate(action: NavigationAction) {
    if (action === "menu") {
      setMenu((value) => !value);
      return;
    }
    if (menu) {
      navigateMenu(action);
      return;
    }
    if (action === "cancel") {
      changeView("platforms");
      return;
    }
    if (view === "platforms") {
      navigatePlatform(action);
      return;
    }
    navigateGames(action);
  }
  function navigateMenu(action: NavigationAction) {
    if (action === "cancel") {
      setMenu(false);
    }
    if (action === "up" || action === "down") {
      setMenuIndex(
        (value) =>
          (value + (action === "up" ? -1 : 1) + views.length) % views.length,
      );
    }
    if (action === "confirm") {
      changeView(views[menuIndex].id);
    }
  }
  function navigatePlatform(action: NavigationAction) {
    const count = directories.data?.items.length ?? 1;
    if (action === "left" || action === "right") {
      setDirectory(
        (value) => (value + (action === "left" ? -1 : 1) + count) % count,
      );
    }
    if (action === "confirm") {
      changeView("games");
    }
  }
  function navigateGames(action: NavigationAction) {
    if (action === "up" || action === "down") {
      setSelected((value) =>
        Math.max(
          0,
          Math.min(
            (entries.data?.items.length ?? 1) - 1,
            value + (action === "up" ? -1 : 1),
          ),
        ),
      );
    }
    if (action === "left" && offset > 0) {
      setOffset(offset - 24);
      setSelected(0);
    }
    if (action === "right" && entries.data?.hasMore) {
      setOffset(offset + 24);
      setSelected(0);
    }
    if (action === "confirm") {
      launch();
    }
    if (action === "favorite") {
      void favorite();
    }
  }
  const controller = useImmersiveNavigation(navigate);
  useEffect(() => {
    document
      .querySelector<HTMLElement>(`.${styles.selectedGame}`)
      ?.scrollIntoView({ block: "nearest" });
  }, [selected]);
  const title = viewTitle(view, current?.name);
  return (
    <div className={styles.shell}>
      <header className={styles.shellHeader}>
        <div>
          <strong>RETROM</strong>
          <span>{title}</span>
        </div>
        <div>
          <button className="button secondary" onClick={() => setMenu(true)}>
            系统菜单
          </button>
          <Link className="button secondary" href="/">
            退出沉浸模式
          </Link>
        </div>
      </header>
      <main className={styles.shellContent}>
        {controller.message || error ? (
          <p role="status">{controller.message || error}</p>
        ) : null}
        {view === "platforms" ? (
          <ResourceState resource={directories}>
            {(data) => (
              <PlatformCarousel
                directories={data.items}
                selected={directory}
                onSelect={setDirectory}
                onOpen={() => changeView("games")}
              />
            )}
          </ResourceState>
        ) : (
          <>
            <ResourceState resource={entries}>
              {(data) => (
                <ImmersiveGames
                  entries={data.items}
                  selected={selected}
                  onSelect={setSelected}
                  title={title}
                  more={data.hasMore}
                  onPrevious={() => {
                    setOffset(Math.max(0, offset - 24));
                    setSelected(0);
                  }}
                  onNext={() => {
                    setOffset(offset + 24);
                    setSelected(0);
                  }}
                  onLaunch={launch}
                />
              )}
            </ResourceState>
            {view === "favorites" ? (
              <label className="field">
                收藏夹
                <select
                  value={folder}
                  onChange={(event) => {
                    setFolder(event.target.value);
                    setOffset(0);
                    setSelected(0);
                  }}
                >
                  <option value="">全部收藏</option>
                  <option value="unclassified">未分类</option>
                  {folders.data?.items.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.name}
                    </option>
                  ))}
                </select>
              </label>
            ) : null}
          </>
        )}
      </main>
      <footer className={styles.helpBar}>
        <span>
          <kbd data-button="horizontal">↔</kbd>选择
        </span>
        <span>
          <kbd data-button="A">A</kbd>确认
        </span>
        <span>
          <kbd data-button="B">B</kbd>返回
        </span>
        <span>
          <kbd>Y</kbd>收藏
        </span>
        <span>
          <kbd data-button="Select">
            <AppIcon name="menu" />
          </kbd>
          菜单
        </span>
        <span>
          {controller.controller === null ? "按手柄任意按钮连接" : "手柄已连接"}
        </span>
      </footer>
      {menu ? (
        <ImmersiveMenu
          selected={menuIndex}
          onSelect={setMenuIndex}
          onOpen={changeView}
          onClose={() => setMenu(false)}
        />
      ) : null}
    </div>
  );
}
async function loadFolders() {
  return result(await api.GET("/api/v1/favorite-folders"));
}
async function loadEntries(
  view: View,
  directory: string | undefined,
  folder: string,
  offset: number,
): Promise<{ items: ImmersiveEntry[]; hasMore: boolean }> {
  if (view === "platforms") {
    return { items: [], hasMore: false };
  }
  if (view === "saves") {
    const page = result(
      await api.GET("/api/v1/saves", {
        params: { query: { offset, limit: 24 } },
      }),
    );
    return {
      items: page.items.map((save) => ({ game: save.game, save })),
      hasMore: page.offset + page.items.length < page.total,
    };
  }
  if (view === "recent") {
    const page = result(
      await api.GET("/api/v1/recent-games", {
        params: { query: { offset, limit: 24, sort: "recent" } },
      }),
    );
    return {
      items: page.items.map((item) => ({
        game: item.game,
        lastPlayedAtMs: item.lastPlayedAtMs,
      })),
      hasMore: page.offset + page.items.length < page.total,
    };
  }
  const page = await loadGames(view === "favorites" ? "favorites" : "library", {
    q: "",
    offset,
    limit: 24,
    platformInstanceId: view === "games" ? directory : undefined,
    folderId: folder && folder !== "unclassified" ? folder : undefined,
    unclassified: folder === "unclassified" || undefined,
  });
  return {
    items: page.items.map((game) => ({ game })),
    hasMore: page.offset + page.items.length < page.total,
  };
}
const views: Array<{ id: View; label: string }> = [
  { id: "platforms", label: "选择平台" },
  { id: "games", label: "游戏库" },
  { id: "favorites", label: "我的收藏" },
  { id: "saves", label: "我的存档" },
  { id: "recent", label: "最近玩过" },
];
function ImmersiveMenu({
  selected,
  onSelect,
  onOpen,
  onClose,
}: {
  selected: number;
  onSelect: (index: number) => void;
  onOpen: (view: View) => void;
  onClose: () => void;
}) {
  const [audio, setAudio] = useState(getImmersiveAudioPreferences);
  function volume(value: number) {
    const next = { ...audio, gameVolume: value };
    setAudio(next);
    saveImmersiveAudioPreferences(next);
  }
  return (
    <div className={menuStyles.backdrop}>
      <section
        className={menuStyles.menu}
        role="dialog"
        aria-modal="true"
        aria-label="系统菜单"
      >
        <header>
          <h2>系统菜单</h2>
        </header>
        <nav className={menuStyles.options}>
          {views.map((item, index) => (
            <button
              key={item.id}
              className={menuStyles.option}
              data-selected={index === selected}
              onFocus={() => onSelect(index)}
              onClick={() => onOpen(item.id)}
            >
              {item.label}
            </button>
          ))}
        </nav>
        <label className="field">
          游戏音量
          <input
            type="range"
            min="0"
            max="100"
            value={audio.gameVolume * 100}
            onChange={(event) => volume(Number(event.target.value) / 100)}
          />
        </label>
        <button onClick={onClose}>返回浏览</button>
        <Link href="/">退出沉浸模式</Link>
      </section>
    </div>
  );
}

function viewTitle(view: View, name: string | undefined) {
  return view === "games"
    ? (name ?? "游戏库")
    : (views.find((item) => item.id === view)?.label ?? "游戏库");
}
