import Image from "next/image";
import { useEffect, useLayoutEffect, useRef, type ReactNode } from "react";
import { InfiniteScrollFooter } from "@/components/infinite-scroll-footer";
import type { Schema } from "@/lib/api/types";
import { LaunchButton } from "@/features/player/launch-button";
import { BrowserTime } from "@/components/browser-time";
import styles from "./immersive.module.css";
import { ImmersiveSaveCarousel } from "./save-carousel";
import type { ImmersiveSaveSelection } from "./use-save-selection";
import libraryStyles from "./library.module.css";
import { SaveRestoreWarning } from "./save-restore-warning";

import type { ImmersiveEntry } from "./immersive-data";
import type { LetterStart } from "./list-jump";
import type { EntryWindow } from "./use-entry-window";
import { EarlierEntries, LetterIndex } from "./list-window-controls";
import windowStyles from "./list-jump.module.css";
export function ImmersiveGames({
  returnTo,
  entries,
  total,
  resource,
  filter,
  saveView,
  saves,
  selected,
  onSelect,
  title,
  onLaunch,
  overlay,
  letters = [],
  onLetter,
  jumpId,
}: {
  returnTo: string;
  entries: ImmersiveEntry[];
  total: number;
  resource: EntryWindow;
  filter?: ReactNode;
  saveView: boolean;
  saves: ImmersiveSaveSelection;
  selected: number;
  onSelect: (index: number) => void;
  title: string;
  onLaunch: () => void;
  overlay?: ReactNode;
  letters?: readonly LetterStart[];
  onLetter?: (offset: number) => void;
  /** Changes with each jump; the jump target is shown at the top so the rest of that page follows it. */
  jumpId?: number;
}) {
  const list = useRef<HTMLDivElement>(null);
  const selectedButton = useRef<HTMLButtonElement>(null);
  const alignedJump = useRef(jumpId);
  useEffect(() => {
    const container = list.current, button = selectedButton.current;
    if (!container || !button) { return; }
    const area = container.getBoundingClientRect(), item = button.getBoundingClientRect();
    const jumped = jumpId !== undefined && jumpId !== alignedJump.current;
    alignedJump.current = jumpId;
    if (jumped || item.top < area.top) { container.scrollTop += item.top - area.top; }
    else if (item.bottom > area.bottom) { container.scrollTop += item.bottom - area.bottom; }
  }, [selected, jumpId, resource.base]);
  const entry = entries[selected];
  return (
    <div className={styles.gameListView}>
      <section className={styles.gameTitles} aria-label={saveView ? "有存档的游戏" : "游戏列表"}>
        <div>
          <p>游戏资料库</p>
          <h1>{title}</h1>
          <span>{total} 款游戏</span>
          {filter}
        </div>
        <div className={windowStyles.titleListRow}>
        <div className={styles.titleList} ref={list}>
          <EarlierEntries base={resource.base} loading={resource.loadingBefore} onLoad={() => void resource.loadBefore()} scrollRoot={list} />
          {entries.map((item, index) => (
            <button
              key={item.game.id}
              ref={index === selected ? selectedButton : undefined}
              className={index === selected ? styles.selectedGame : ""}
              aria-pressed={index === selected}
              onClick={() => onSelect(index)}
              onDoubleClick={onLaunch}
            >
              <span>{String(resource.base + index + 1).padStart(2, "0")}</span>
              <strong>{item.game.title}</strong>
              {item.game.favorite ? <small className={styles.favoriteIndicator}>♥</small> : null}
            </button>
          ))}
          <InfiniteScrollFooter count={resource.base + entries.length} total={total} {...resource} scrollRoot={list} />
        </div>
        {onLetter ? <LetterIndex letters={letters} current={entry?.game.titleLetter ?? ""} onPick={onLetter} /> : null}
        </div>
        {overlay}
      </section>
      {entry ? (
        <GamePresentation entry={entry} returnTo={returnTo} saves={saveView ? saves : undefined} />
      ) : (
        <div className={styles.centerState}>
          <h2>{saveView ? "这里还没有存档" : "这里还没有游戏"}</h2>
          <p>选择其他目录或分类继续浏览。</p>
        </div>
      )}
    </div>
  );
}
function GamePresentation({ entry, returnTo, saves }: { entry: ImmersiveEntry; returnTo: string; saves?: ImmersiveSaveSelection }) {
  const { game, save } = entry;
  return (
    <section className={`${styles.gameDetails} ${saves ? libraryStyles.saveGameDetails : ""}`}>
      <GameMedia entry={saves ? { game, save: saves.previewSave } : entry} saveView={!!saves} />
      {saves ? <ImmersiveSaveCarousel gameTitle={game.title} selection={saves} returnTo={returnTo} /> : <div className={styles.descriptionPanel}>
        <header className={styles.descriptionHeading}>
          <div>
            <p>{game.directoryName} {game.releaseYear ?? ""}</p>
            <h2 title={game.title}>{game.title}</h2>
          </div>
          <LaunchButton
            gameId={game.id}
            coreId={save?.extinfo.coreId}
            saveId={save?.id} restoreWarning={save?.restoreWarning}
            disabled={!!save && !save.restorable}
            returnTo={returnTo}
          >
            {save ? "从存档继续" : "开始游戏"}
          </LaunchButton>
        </header>
        <WholeLines key={game.id}><GameSynopsis entry={entry} /></WholeLines>
        {save?.kind === "game_save" ? (
          <p>进入游戏后，请在游戏内读取存档。</p>
        ) : null}
      </div>}
    </section>
  );
}
function GameMedia({ entry: { game, save }, saveView = false }: { entry: ImmersiveEntry; saveView?: boolean }) {
  const cover = game.media.find((item) => item.kind === "cover");
  const video = saveView ? undefined : game.media.find((item) => item.kind === "video");
  const artwork = saveView ? save?.screenshotUrl : cover?.url;
  return (
      <div
        className={`${styles.mediaStage} ${video ? styles.withVideo : styles.coverOnly}`}
      >
        {artwork ? <div key={artwork} className={styles.mediaBackdrop} aria-hidden="true">
          <Image src={artwork} fill sizes="20vw" alt="" unoptimized />
        </div> : null}
        <div className={saveView ? styles.savePreview : styles.poster}>
          {artwork ? (
            <Image
              src={artwork}
              fill
              sizes="40vw"
              alt={game.title}
              unoptimized
            />
          ) : (
            <div className={styles.mediaPlaceholder}>
              <strong>{game.title}</strong>
            </div>
          )}
        </div>
        {video ? (
          <div className={styles.videoPanel}>
            <video src={video.url} controls playsInline preload="metadata" />
          </div>
        ) : null}
        {save ? <SaveRestoreWarning save={save} /> : null}
      </div>
  );
}
const restoreMessages: Record<Schema<"Save">["restoreReason"], string> = {
  "": "",
  game_unavailable: "游戏当前不可用",
  save_unavailable: "存档文件当前不可用",
  core_unavailable: "保存时的核心当前不可用。",
  content_changed: "游戏内容已变化，无法恢复。",
  format_unreadable: "当前核心无法读取此存档格式。",
};

/** Shows only complete lines of the last paragraph; the full text stays in its tooltip. */
function WholeLines({ children }: { children: ReactNode }) {
  const box = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const container = box.current;
    if (!container) { return; }
    const fit = () => {
      const last = container.lastElementChild;
      if (!(last instanceof HTMLElement)) { return; }
      const lineHeight = Number.parseFloat(getComputedStyle(last).lineHeight) || 1;
      const lines = Math.floor((container.clientHeight - last.offsetTop) / lineHeight);
      container.style.setProperty("--description-lines", String(Math.max(lines, 1)));
      container.dataset.lines = String(Math.max(lines, 0));
      last.title = last.textContent ?? "";
    };
    fit();
    const observer = new ResizeObserver(fit);
    observer.observe(container);
    return () => observer.disconnect();
  }, []);
  return <div className={styles.description} ref={box}>{children}</div>;
}

function GameSynopsis({ entry }: { entry: ImmersiveEntry }) {
  const { game, save } = entry;
  return (
    <>
      {" "}
      {save ? (
        <>
          <p>
            {save.name} · <BrowserTime value={save.updatedAtMs} />
          </p>
          {!save.restorable ? (
            <p role="status">{restoreMessages[save.restoreReason]}</p>
          ) : null}
        </>
      ) : (
        <>
          {entry.lastPlayedAtMs ? <p>上次游玩：<BrowserTime value={entry.lastPlayedAtMs} /></p> : null}
          <p>{game.description || "暂无游戏简介。"}</p>
        </>
      )}
    </>
  );
}
