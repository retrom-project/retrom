import Link from "next/link";
import { AppIcon } from "@/components/app-icon";
import { LaunchButton } from "@/features/player/launch-button";
import { formatTime } from "@/lib/backend";
import { featuredLaunchDOSEntry, type FeaturedGame } from "./home-data";
import { HomeFeaturedMedia } from "./home-featured-media";

function EmptyFeatured({ gameCount }: { gameCount: number }) {
    return <article className="panel home-featured-panel home-featured-empty">
      <div className="home-featured-copy">
        <p className="home-featured-kicker">你的下一场冒险</p>
        <h2>{gameCount ? "挑一款，开始冒险。" : "收藏，从第一款开始。"}</h2>
        <p className="home-featured-intro">{gameCount ? "玩过的游戏会留在这里，方便下次回来。" : "游戏添加好后，就能在这里找到。"}</p>
        <Link className="button" href="/library">浏览游戏库</Link>
      </div>
    </article>;
}

export function HomeFeatured({ game, gameCount = 0, phone = false }: { game: FeaturedGame | null; gameCount?: number; phone?: boolean }) {
  if (!game) {return <EmptyFeatured gameCount={gameCount} />;}
  const save = game.lastSessionSave;
  return <article className="panel home-featured-panel">
    {!phone ? <HomeFeaturedMedia screenshotUrl={save?.screenshotUrl ?? null} coverUrl={game.coverUrl} platformId={game.platform.id} title={game.title} /> : null}
    <div className="home-featured-copy">
      <p className="home-featured-kicker"><AppIcon name="history" />{save ? "继续上次的冒险" : "最近玩过"}</p>
      <div className="home-featured-details"><h2>{game.title}</h2></div>
      <FeaturedMetadata game={game} />
      <div className="home-featured-actions">
        <div className="home-launch-control"><LaunchButton gameId={game.gameId} saveStateId={save?.saveStateId ?? null} dosEntry={featuredLaunchDOSEntry(game)} returnTo="/" label={save ? "从存档继续" : phone ? "开始游戏" : "再玩一次"} /></div>
        <Link className="home-detail-link" href={`/games/${game.gameId}`}>查看游戏详情</Link>
      </div>
      <p className="home-launch-note">本次将从{save ? "存档位置" : "游戏开头"}启动</p>
      {game.hasSaveStates ? <Link className="home-save-link" href={`/saves?gameId=${encodeURIComponent(game.gameId)}`}>查看存档</Link> : null}
    </div>
  </article>;
}

function FeaturedMetadata({ game }: { game: FeaturedGame }) {
  const save = game.lastSessionSave;
  const time = save ? `手动存档 · ${formatTime(save.createdAtMs)}` : `${formatTime(game.lastPlayedAtMs)} 玩过`;
  return <div className="home-featured-meta"><span className="home-featured-platform">{game.platform.name}</span><span>{time}{save?.discLabel ? ` · ${save.discLabel}` : ""}</span></div>;
}
