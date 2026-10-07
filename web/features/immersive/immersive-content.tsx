import Image from "next/image";
import type { Schema } from "@/lib/api/types";
import { LaunchButton } from "@/features/player/launch-button";
import { BrowserTime } from "@/components/browser-time";
import styles from "./immersive.module.css";
import platform from "./platform.module.css";
import library from "./library.module.css";
export type ImmersiveEntry = {
  game: Schema<"Game">;
  save?: Schema<"Save">;
  lastPlayedAtMs?: number;
};
export function PlatformCarousel({
  directories,
  selected,
  onSelect,
  onOpen,
}: {
  directories: Schema<"Directory">[];
  selected: number;
  onSelect: (index: number) => void;
  onOpen: () => void;
}) {
  if (!directories.length) {
    return (
      <div className={styles.centerState}>
        <h1>还没有游戏目录</h1>
        <p>游戏发布后会显示在这里。</p>
      </div>
    );
  }
  return (
    <div className={platform.platformView}>
      <header className={styles.viewHeading}>
        <p>选择平台</p>
        <h1>下一场冒险</h1>
      </header>
      <div className={platform.platformStage}>
        <div className={platform.platformCarousel}>
          {[-1, 0, 1].map((shift) => {
            const index =
              (selected + shift + directories.length) % directories.length;
            const directory = directories[index];
            return (
              <button
                key={shift}
                className={`${platform.platformCard} ${shift === 0 ? platform.currentPlatform : ""} ${platform[`platformTone${index % 5}`]}`}
                onClick={() => {
                  if (shift === 0) {
                    onOpen();
                  } else {
                    onSelect(index);
                  }
                }}
              >
                <div className={platform.platformCopy}>
                  <p className={platform.platformCode}>
                    {directory.platformId.toUpperCase()}
                  </p>
                  <p>游戏目录</p>
                  <h2>{directory.name}</h2>
                  <strong>{directory.gameCount} 款游戏</strong>
                  <span>按 A 或回车浏览游戏</span>
                </div>
                {shift === 0 ? (
                  <Image
                    src={`/images/platforms/${directory.platformId}.svg`}
                    width={300}
                    height={300}
                    alt=""
                    unoptimized
                  />
                ) : null}
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}
export function ImmersiveGames({
  entries,
  selected,
  onSelect,
  title,
  more,
  onPrevious,
  onNext,
  onLaunch,
}: {
  entries: ImmersiveEntry[];
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
      <section className={styles.gameTitles}>
        <div>
          <p>游戏资料库</p>
          <h1>{title}</h1>
          <span>{entries.length} 款游戏</span>
        </div>
        <div className={styles.titleList}>
          {entries.map((item, index) => (
            <button
              key={item.save?.id ?? item.game.id}
              className={index === selected ? styles.selectedGame : ""}
              aria-pressed={index === selected}
              onClick={() => onSelect(index)}
              onDoubleClick={onLaunch}
            >
              <span>{String(index + 1).padStart(2, "0")}</span>
              <strong>{item.game.title}</strong>
              <small>
                {item.save ? item.save.name : item.game.favorite ? "♥" : ""}
              </small>
            </button>
          ))}
          <div>
            <button onClick={onPrevious}>上一页</button>
            <button disabled={!more} onClick={onNext}>
              下一页
            </button>
          </div>
        </div>
      </section>
      {entry ? (
        <GamePresentation entry={entry} />
      ) : (
        <div className={styles.centerState}>
          <h2>这里还没有游戏</h2>
          <p>选择其他目录或分类继续浏览。</p>
        </div>
      )}
    </div>
  );
}
function GamePresentation({ entry }: { entry: ImmersiveEntry }) {
  const { game, save } = entry;
  const cover = game.media.find((item) => item.kind === "cover");
  const video = game.media.find((item) => item.kind === "video");
  return (
    <section className={styles.gameDetails}>
      <div
        className={`${styles.mediaStage} ${video ? styles.withVideo : styles.coverOnly}`}
      >
        <div className={styles.poster}>
          {cover ? (
            <Image
              src={cover.url}
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
      </div>
      <div className={save ? library.saveDetails : styles.descriptionPanel}>
        <h2>{game.title}</h2>
        <p>
          {game.directoryName} {game.releaseYear ?? ""}
        </p>
        <GameSynopsis entry={entry} />{" "}
        <LaunchButton
          gameId={game.id}
          coreId={save?.extinfo.coreId}
          saveId={save?.id}
          disabled={!!save && !save.restorable}
          returnTo="/immersive"
        >
          {save ? "从存档继续" : "开始游戏"}
        </LaunchButton>
        {save?.kind === "game_save" ? (
          <p>进入游戏后，请在游戏内读取存档。</p>
        ) : null}
      </div>
    </section>
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
          {save.screenshotUrl ? (
            <div className={library.saveScreenshot}>
              <Image
                src={save.screenshotUrl}
                fill
                unoptimized
                alt="存档截图"
                sizes="50vw"
              />
            </div>
          ) : null}
          {!save.restorable ? (
            <p role="status">{restoreMessages[save.restoreReason]}</p>
          ) : null}
        </>
      ) : (
        <p>{game.description || "暂无游戏简介。"}</p>
      )}
    </>
  );
}
