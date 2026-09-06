import Image from "next/image";
import Link from "next/link";
import { AppIcon } from "@/components/app-icon";
import type { FeaturedGame, Home } from "@/features/home/home-data";
import { LaunchButton } from "@/features/player/launch-button";
import { formatTime } from "@/lib/backend";

function Poster({ title, coverUrl }: { title: string; coverUrl: string | null }) {
  return <span className="phone-game-poster">
    {coverUrl ? <Image src={coverUrl} alt={`${title} 封面`} fill sizes="(max-width: 479px) 45vw, 30vw" unoptimized /> : <span>{title}</span>}
  </span>;
}

function ContinueGame({ game }: { game: FeaturedGame }) {
  const save = game.lastSessionSave;
  return <section className="phone-continue" aria-labelledby="phone-continue-title">
    <h1 id="phone-continue-title">{save ? "继续游玩" : "再玩一局"}</h1>
    <div className="phone-continue-card">
      <Link href={`/games/${game.gameId}`} aria-label={`查看${game.title}游戏详情`}><Poster title={game.title} coverUrl={game.coverUrl} /></Link>
      <div className="phone-continue-copy">
        <div className="phone-continue-info">
          <Link href={`/games/${game.gameId}`}><h2>{game.title}</h2></Link>
          <p>{game.platform.name}</p>
          <small>{save ? `存档 · ${formatTime(save.createdAtMs)}` : "本次从游戏开头开始"}</small>
        </div>
        <div className="phone-continue-actions"><LaunchButton gameId={game.gameId} saveStateId={save?.saveStateId ?? null} returnTo="/" label={save ? "从存档继续" : "开始游戏"} /></div>
      </div>
    </div>
  </section>;
}

export function MobileHome({ home }: { home: Home }) {
  const recent = home.recentGames.filter((game) => game.gameId !== home.featuredGame?.gameId).slice(0, 6);
  const showRecent = Boolean(home.featuredGame && recent.length);
  const games = showRecent ? recent : home.latestGames.filter((game) => game.gameId !== home.featuredGame?.gameId).slice(0, 6);
  const sectionTitle = showRecent ? "最近游戏" : home.featuredGame ? "发现游戏" : "从这里开始";
  return <div className="phone-home">
    <form action="/library" role="search" className="phone-home-search">
      <label className="sr-only" htmlFor="phone-game-search">搜索游戏</label>
      <AppIcon name="search" />
      <input id="phone-game-search" type="search" name="q" placeholder="想玩什么？搜一下" />
      <button type="submit">搜索</button>
    </form>
    {home.featuredGame ? <ContinueGame game={home.featuredGame} /> : <section className="phone-welcome">
      <h1>{home.library.gameCount ? "挑一款游戏，开始玩" : "游戏库还是空的"}</h1>
      <p>{home.library.gameCount ? "玩过的游戏会留在这里，方便下次回来。" : "游戏添加好后，就能在这里找到。"}</p>
      <Link className="button" href="/library">浏览游戏库</Link>
    </section>}
    {games.length > 0 ? <section aria-labelledby="phone-recent-title">
      <div className="phone-section-head"><h2 id="phone-recent-title">{sectionTitle}</h2><Link href={showRecent ? "/recent" : "/library"}>查看全部</Link></div>
      <div className="phone-game-grid">
        {games.map((game) => <Link href={`/games/${game.gameId}`} className="phone-game-card" key={game.gameId}>
          <Poster title={game.title} coverUrl={game.coverUrl} /><strong>{game.title}</strong><small>{game.platform.name}</small>
        </Link>)}
      </div>
    </section> : null}
  </div>;
}
