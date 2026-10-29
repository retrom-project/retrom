import { useHorizontalWheel } from "@/lib/use-horizontal-wheel";
import type { ReactNode } from "react";
import Image from "next/image";
import Link from "next/link";
import type { Directory, Game, Schema } from "@/lib/api/types";
import { AppIcon } from "@/components/app-icon";
import { LaunchButton } from "@/features/player/launch-button";
import { BrowserTime } from "@/components/browser-time";
import { HomePlatformArt } from "./home-platform-art";
import { HomeFeaturedMedia } from "./home-featured-media";
import type { HomeData } from "./home-data";
export function featuredGameId(data: Schema<"Home">) {
  return data.recent[0]?.game.id;
}
export function HomeFeatured({ data }: { data: HomeData }) {
  const featured = data.featured;
  if (!featured) {
    return (
      <article className="panel home-featured-panel home-featured-empty">
        <div className="home-featured-copy">
          <p className="home-featured-kicker">你的下一场冒险</p>
          <h2>挑一款，开始冒险。</h2>
          <p className="home-featured-intro">
            玩过的游戏会留在这里，方便下次回来。
          </p>
          <Link className="button" href="/library">
            浏览游戏库
          </Link>
        </div>
      </article>
    );
  }
  const { recent, save } = featured;
  const game = recent.game;
  return (
    <article className="panel home-featured-panel">
      <HomeFeaturedMedia key={`${game.id}:${save?.id}:${save?.version}`} game={game} save={save} />
      <div className="home-featured-copy">
        <p className="home-featured-kicker">
          <AppIcon name="history" />
          {save ? "继续上次的冒险" : "最近玩过"}
        </p>
        <h2 title={game.title}>{game.title}</h2>
        <div className="home-featured-meta">
          <span className="home-featured-platform">
            <span>{game.directoryName}</span>
          </span>
          <span>
            {save ? <>{save.kind === "checkpoint" ? "即时存档" : "游戏内存档"} · <BrowserTime value={save.updatedAtMs} /></>
              : <>上次游玩 · <BrowserTime value={recent.lastPlayedAtMs} /></>}
          </span>
        </div>
        <div className="home-featured-actions">
          <LaunchButton
            gameId={game.id}
            saveId={save?.id} restoreWarning={save?.restoreWarning}
            coreId={save?.extinfo.coreId}
            disabled={save ? !save.restorable : false}
            returnTo="/"
          >
            {save ? "从存档继续" : <span className="home-launch-label">
              <span className="home-launch-label-sizer" aria-hidden="true">从存档继续</span>
              <span>新的开始</span>
            </span>}
          </LaunchButton>
          <Link className="home-detail-link" href={`/games/${game.id}`}>
            查看游戏详情
          </Link>
        </div>
        <p className="home-launch-note">
          {launchNote(save)}
        </p>
        <Link className="home-save-link" href={`/saves?gameId=${game.id}`}>
          查看存档
        </Link>
      </div>
    </article>
  );
}
function launchNote(save: Schema<"Save"> | undefined) {
  if (save && !save.restorable) { return "最新存档暂不可恢复，可在“查看存档”中查看详情。"; }
  if (save?.restoreWarning === "core_changed") { return "核心版本已变化，确认后尝试恢复。"; }
  if (save?.kind === "game_save") { return "启动后请在游戏内读取进度。"; }
  return save ? "本次将从存档位置启动" : "本次将启动全新游戏";
}
export function HomeRecent({ recent }: { recent: Schema<"RecentGame">[] }) {
  return (
    <section className="home-layer">
      <div className="home-section-head">
        <h2>最近玩过</h2>
        <Link href="/recent">查看全部</Link>
      </div>
      {recent.length ? (
        <HomeGameRail items={recent.map((item) => ({
          game: item.game, caption: <BrowserTime value={item.lastPlayedAtMs} />,
        }))} />
      ) : (
        <div className="home-inline-empty">最近玩过的游戏会出现在这里。</div>
      )}
    </section>
  );
}
export function HomeGameRail({ items }: { items: { game: Game; caption: ReactNode }[] }) {
  const rail = useHorizontalWheel<HTMLDivElement>();
  return (
    <div ref={rail} className="home-horizontal-rail home-recent-rail">
      {items.map(({ game, caption }) => (
        <Link className="home-recent-card" key={game.id} href={`/games/${game.id}`}>
          <span className="home-recent-cover">
            <Cover game={game} />
            <span className="home-poster-platform"><span>{game.directoryName}</span></span>
          </span>
          <span className="home-recent-copy">
            <strong title={game.title}>{game.title}</strong>
            <small>{caption}</small>
          </span>
        </Link>
      ))}
    </div>
  );
}
export function HomeFavorites({ games }: { games: Game[] }) {
  const favoritesRail = useHorizontalWheel<HTMLDivElement>();
  return (
    <section className="home-layer home-favorites">
      <div className="home-favorites-head">
        <h2>一直喜欢的</h2>
        <Link href="/favorites">查看全部</Link>
      </div>
      <div className="home-favorites-body">
        {games.length ? (
          <div ref={favoritesRail} className="home-favorites-list">
            {games.slice(0, 3).map((game) => (
              <Link
                className="home-favorite-game"
                key={game.id}
                href={`/games/${game.id}`}
              >
                <span className="home-favorite-cover">
                  {game.media.some((media) => media.kind === "cover") ? (
                    <Cover game={game} />
                  ) : (
                    <span aria-hidden="true">R</span>
                  )}
                </span>
                <span className="home-favorite-copy">
                  <strong>{game.title}</strong>
                  <small>{game.directoryName}</small>
                </span>
              </Link>
            ))}
          </div>
        ) : (
          <div className="home-favorites-empty">
            <AppIcon name="heart" />
            <div>
              <strong>把喜欢的游戏留在这里</strong>
              <p>在游戏卡片上点亮爱心，下次从这里出发。</p>
              <Link href="/library">浏览游戏库</Link>
            </div>
          </div>
        )}
      </div>
    </section>
  );
}
export function HomeDirectories({ directories }: { directories: Directory[] }) {
  const platformRail = useHorizontalWheel<HTMLDivElement>();
  const populatedDirectories = directories.filter((directory) => directory.gameCount > 0);
  if (!populatedDirectories.length) { return null; }
  return (
    <section className="home-layer">
      <div className="home-section-head">
        <h2>换个平台逛逛</h2>
        <Link href="/library">浏览全部</Link>
      </div>
      <div ref={platformRail} className="home-horizontal-rail home-platform-rail">
        {populatedDirectories.map((directory) => (
          <article className="home-platform-card" key={directory.id}>
            <Link href={`/library?platformInstanceId=${directory.id}`}>
              <HomePlatformArt key={directory.platformId} platformId={directory.platformId} />
              <span>
                <strong>{directory.name}</strong>
                <small>{directory.gameCount} 款游戏</small>
              </span>
            </Link>
          </article>
        ))}
      </div>
    </section>
  );
}
function Cover({ game }: { game: Game }) {
  const cover = game.media.find((media) => media.kind === "cover");
  return cover ? (
    <Image src={cover.url} alt="" fill sizes="240px" unoptimized />
  ) : (
    <span className="home-poster-placeholder">
      <small>{game.platformId}</small>
      <span>{game.title}</span>
    </span>
  );
}
