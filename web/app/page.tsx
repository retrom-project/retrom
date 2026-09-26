import Link from "next/link";
import { AppIcon } from "@/components/app-icon";
import { PageHeader } from "@/components/ui";
import { HomeFeatured } from "@/features/home/home-featured";
import { HomeGameCard } from "@/features/home/home-game-card";
import { HomeFavorites } from "@/features/home/home-favorites";
import { HorizontalRail, PlatformRail } from "@/features/home/home-rails";
import { ImmersiveHomeEntry } from "@/features/home/immersive-home-entry";
import type { Home } from "@/features/home/home-data";
import { ImmersiveEntryDialog } from "@/features/immersive/entry-dialog";
import { MobileHome } from "@/features/mobile/mobile-home";
import { PhoneLayout } from "@/features/mobile/phone-layout";
import { backendJSON } from "@/lib/server-backend";

export const metadata = { title: "首页" };

function duration(value: number) {
  if (value < 60_000) {return "少于 1 分钟";}
  const hours = value / 3_600_000;
  return hours < 1 ? `${Math.floor(value / 60_000)} 分钟` : `${hours.toFixed(hours < 10 ? 1 : 0)} 小时`;
}

export default async function HomePage() {
  const home = await backendJSON<Home>("/api/v1/home");
  const firstVisit = !home.featuredGame;
  const games = firstVisit ? home.latestGames : home.recentGames.filter((game) => game.gameId !== home.featuredGame?.gameId);
  const platforms = home.platforms.filter((platform) => platform.gameCount > 0);
  return <><ImmersiveEntryDialog /><PhoneLayout phone={<MobileHome home={home} />}><div className="page-layout page-layout-home home-page">
    <section className="home-layer home-hero-layer" data-home-layer="1" aria-label="今天玩什么">
      <PageHeader title="今天，玩点什么？" description="继续上次的冒险，发现下一款心头好。" actions={<>
        <form action="/library" role="search" className="home-search"><AppIcon name="search" /><label className="sr-only" htmlFor="home-game-search">搜索游戏</label><input id="home-game-search" name="q" type="search" placeholder="搜索游戏…" /></form>
        <ImmersiveHomeEntry />
      </>} />
      <HomeFeatured game={home.featuredGame} gameCount={home.library.gameCount} />
    </section>
    <section className="home-layer home-recent-section" data-home-layer="2">
      <div className="home-section-head"><h2>{firstVisit ? "从这里开始" : "最近游玩"}</h2><Link href={firstVisit ? "/library" : "/recent"}>查看全部</Link></div>
      {games.length ? <HorizontalRail className="home-recent-rail" label={firstVisit ? "游戏库中的游戏" : "最近游玩的游戏"}>{games.map((game) => <HomeGameCard game={game} key={game.gameId} />)}</HorizontalRail>
        : <div className="home-inline-empty">{firstVisit ? "游戏添加好后，就能在这里找到。" : "玩过的其他游戏会出现在这里。"}</div>}
    </section>
    {home.library.gameCount > 0 ? <HomeFavorites /> : null}
    {platforms.length ? <section className="home-layer home-platform-section" data-home-layer="4">
      <div className="home-section-head"><h2>换个平台逛逛</h2><Link href="/library">进入游戏库</Link></div>
      <PlatformRail platforms={platforms} />
    </section> : null}
    <section className="home-layer home-summary" data-home-layer="5" aria-label="我的资料库">
      <strong>我的资料库</strong><span><b>{home.library.gameCount}</b> 款游戏</span><span><b>{home.library.saveStateCount}</b> 份存档</span><span><b>{duration(home.play.activeDurationMs)}</b> 累计游玩</span>
    </section>
  </div></PhoneLayout></>;
}
