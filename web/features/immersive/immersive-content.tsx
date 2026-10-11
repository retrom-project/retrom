import Image from "next/image";
import type { ReactNode } from "react";
import type { Schema } from "@/lib/api/types";
import { LaunchButton } from "@/features/player/launch-button";
import { BrowserTime } from "@/components/browser-time";
import styles from "./immersive.module.css";
import { ImmersiveSaveCarousel } from "./save-carousel";
import type { ImmersiveSaveSelection } from "./use-save-selection";
import libraryStyles from "./library.module.css";
import { SaveRestoreWarning } from "./save-restore-warning";

import type { ImmersiveEntry } from "./immersive-data";
export function ImmersiveGames({
  returnTo,
  entries,
  total,
  offset,
  filter,
  saveView,
  saves,
  selected,
  onSelect,
  title,
  more,
  onPrevious,
  onNext,
  onLaunch,
}: {
  returnTo: string;
  entries: ImmersiveEntry[];
  total: number;
  offset: number;
  filter?: ReactNode;
  saveView: boolean;
  saves: ImmersiveSaveSelection;
  selected: number;
  onSelect: (index: number) => void;
  title: string;
  more: boolean;
  onPrevious: () => void;
  onNext: () => void;
  onLaunch: () => void;
}) {
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
        <div className={styles.titleList}>
          {entries.map((item, index) => (
            <button
              key={item.game.id}
              className={index === selected ? styles.selectedGame : ""}
              aria-pressed={index === selected}
              onClick={() => onSelect(index)}
              onDoubleClick={onLaunch}
            >
              <span>{String(offset + index + 1).padStart(2, "0")}</span>
              <strong>{item.game.title}</strong>
              {item.game.favorite ? <small className={styles.favoriteIndicator}>♥</small> : null}
            </button>
          ))}
        </div>
        <div className={styles.listPaging}>
          <button className="button secondary" disabled={offset === 0} onClick={onPrevious}>上一页</button>
          <button className="button secondary" disabled={!more} onClick={onNext}>下一页</button>
        </div>
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
      <GameMedia entry={saves ? { game, save: saves.save } : entry} />
      {saves ? <ImmersiveSaveCarousel gameTitle={game.title} selection={saves} returnTo={returnTo} /> : <div className={styles.descriptionPanel}>
        <p>{game.directoryName} {game.releaseYear ?? ""}</p>
        <h2>{game.title}</h2>
        <div className={styles.description}><GameSynopsis entry={entry} /></div>
        <LaunchButton
          gameId={game.id}
          coreId={save?.extinfo.coreId}
          saveId={save?.id}
          disabled={!!save && !save.restorable}
          returnTo={returnTo}
        >
          {save ? "从存档继续" : "开始游戏"}
        </LaunchButton>
        {save?.kind === "game_save" ? (
          <p>进入游戏后，请在游戏内读取存档。</p>
        ) : null}
      </div>}
    </section>
  );
}
function GameMedia({ entry: { game, save } }: { entry: ImmersiveEntry }) {
  const cover = game.media.find((item) => item.kind === "cover");
  const video = save ? undefined : game.media.find((item) => item.kind === "video");
  const artwork = save?.screenshotUrl ?? cover?.url;
  return (
      <div
        className={`${styles.mediaStage} ${video ? styles.withVideo : styles.coverOnly}`}
      >
        <div className={save?.screenshotUrl ? styles.savePreview : styles.poster}>
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
  core_changed: "运行核心已变化，无法恢复。",
  content_changed: "游戏内容已变化，无法恢复。",
  format_unreadable: "当前核心无法读取此存档格式。",
};

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
