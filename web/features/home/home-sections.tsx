import { useHorizontalWheel } from "@/lib/use-horizontal-wheel";
import Image from "next/image";
import Link from "next/link";
import type { Directory, Game, Schema } from "@/lib/api/types";
import { AppIcon } from "@/components/app-icon";
import { LaunchButton } from "@/features/player/launch-button";
import { BrowserTime } from "@/components/browser-time";
import { HomePlatformArt } from "./home-platform-art";
export function featuredGameId(data: Schema<"Home">) {
  return (
    data.saves.find((save) => save.restorable)?.game.id ??
    data.recent[0]?.game.id
  );
}
export function HomeFeatured({ data }: { data: Schema<"Home"> }) {
  const save = data.saves.find((item) => item.restorable);
  const game = save?.game ?? data.recent[0]?.game;
  if (!game) {
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
  const image = featuredImage(game, save);
  return (
    <article className="panel home-featured-panel">
      {image ? (
        <div className="home-featured-media">
          <Image
            className="home-featured-art is-ready"
            src={image}
            fill
            unoptimized
            alt=""
            sizes="60vw"
          />
        </div>
      ) : null}
      <div className="home-featured-copy">
        <p className="home-featured-kicker">
          <AppIcon name="history" />
          {save ? "继续上次的冒险" : "最近玩过"}
        </p>
        <h2>{game.title}</h2>
        <div className="home-featured-meta">
          <span className="home-featured-platform">
            <span>{game.directoryName}</span>
          </span>
          <span>
            {save ? (
              <>
                {save.kind === "checkpoint" ? "即时存档" : "游戏内存档"} ·{" "}
                <BrowserTime value={save.updatedAtMs} />
              </>
            ) : (
              "最近玩过"
            )}
          </span>
        </div>
        <div className="home-featured-actions">
          <LaunchButton
            gameId={game.id}
            saveId={save?.id}
            coreId={save?.extinfo.coreId}
            returnTo="/"
          >
            {save ? "从存档继续" : "再玩一次"}
          </LaunchButton>
          <Link className="home-detail-link" href={`/games/${game.id}`}>
            查看游戏详情
          </Link>
        </div>
        <p className="home-launch-note">
          本次将从{save ? "存档位置" : "游戏开头"}启动
        </p>
        <Link className="home-save-link" href={`/saves?gameId=${game.id}`}>
          查看存档
        </Link>
      </div>
    </article>
  );
}
export function HomeRecent({ recent }: { recent: Schema<"RecentGame">[] }) {
  const recentRail = useHorizontalWheel<HTMLDivElement>();
  return (
    <section className="home-layer">
      <div className="home-section-head">
        <h2>最近玩过</h2>
        <Link href="/recent">查看全部</Link>
      </div>
      {recent.length ? (
        <div ref={recentRail} className="home-horizontal-rail home-recent-rail">
          {recent.map((item) => (
            <Link
              className="home-recent-card"
              key={item.game.id}
              href={`/games/${item.game.id}`}
            >
              <span className="home-recent-cover">
                <Cover game={item.game} />
                <span className="home-poster-platform">
                  <span>{item.game.directoryName}</span>
                </span>
              </span>
              <span className="home-recent-copy">
                <strong>{item.game.title}</strong>
                <small>
                  <BrowserTime value={item.lastPlayedAtMs} />
                </small>
              </span>
            </Link>
          ))}
        </div>
      ) : (
        <div className="home-inline-empty">最近玩过的游戏会出现在这里。</div>
      )}
    </section>
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
  return (
    <section className="home-layer">
      <div className="home-section-head">
        <h2>换个平台逛逛</h2>
        <Link href="/library">浏览全部</Link>
      </div>
      <div ref={platformRail} className="home-horizontal-rail home-platform-rail">
        {directories.map((directory) => (
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
      {!directories.length ? (
        <div className="home-inline-empty">
          管理员可以从运行声明创建游戏目录。
        </div>
      ) : null}
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

function featuredImage(game: Game, save: Schema<"Save"> | undefined) {
  return (
    save?.screenshotUrl ||
    game.media.find((media) => media.kind === "cover")?.url
  );
}
