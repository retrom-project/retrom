import Image from "next/image";
import Link from "next/link";
import { AppIcon } from "@/components/app-icon";
import { EmptyState, PageHeader } from "@/components/ui";
import { HorizontalRail, PlatformRail, type HomePlatform } from "@/features/home/home-rails";
import { LaunchButton } from "@/features/player/launch-button";
import { backendJSON, formatTime } from "@/lib/backend";

export const metadata = { title: "首页" };

type RecentGame = {
  gameId: string;
  title: string;
  platform: { id: string; name: string };
  platformInstance: { id: string; name: string };
  lastPlayedAtMs: number;
  activeDurationMs: number;
  sessionCount: number;
  coverUrl: string | null;
};

type FeaturedGame = RecentGame & {
  hasSaveStates: boolean;
  lastSessionSave: null | {
    saveStateId: string;
    createdAtMs: number;
    activeDurationMs: number;
    screenshotUrl: string;
  };
};

type Home = {
  library: { gameCount: number; saveStateCount: number };
  play: { activeDurationMs: number };
  featuredGame: FeaturedGame | null;
  recentGames: RecentGame[];
  platforms: HomePlatform[];
  quickPlatforms: HomePlatform[];
};

function duration(value: number) {
  if (value < 60_000) return "少于 1 分钟";
  const hours = value / 3_600_000;
  return hours < 1 ? `${Math.floor(value / 60_000)} 分钟` : `${hours.toFixed(hours < 10 ? 1 : 0)} 小时`;
}

function platformCode(id: string) {
  if (id === "arcade") return "ARC";
  return id.replaceAll(/[^a-z0-9]/gi, "").slice(0, 4).toUpperCase();
}

function FeaturedGamePanel({ game }: { game: FeaturedGame | null }) {
  if (!game) {
    return <article className="panel home-featured-panel home-featured-empty">
      <div className="home-panel-head"><div><p className="eyebrow">最近玩的游戏</p><h2>从第一款游戏开始</h2><p>游玩记录会在这里变成快捷入口</p></div></div>
      <EmptyState title="还没有游玩记录" description="从游戏库选择一款游戏，下一次回来就能从这里快速开始。" />
    </article>;
  }
  const sessionSave = game.lastSessionSave;
  return <article className="panel home-featured-panel">
    <div className="home-panel-head home-featured-head">
      <div><p className="eyebrow">最近玩的游戏</p><h2>{sessionSave ? "继续刚才的那一局" : "回到刚才的那一局"}</h2><p>{sessionSave ? "上次游玩已经留下存档，可以接着进度继续" : "这次没有留下存档，随时可以再来一局"}</p></div>
      {game.hasSaveStates ? <Link className="home-save-link" href={`/saves?gameId=${encodeURIComponent(game.gameId)}`}>查看存档</Link> : <span className="home-save-status"><i aria-hidden="true" />暂无可恢复存档</span>}
    </div>
    <div className="home-featured-body">
      <div className={`home-featured-media${sessionSave ? " has-session-save" : ""}`}>
        {game.coverUrl ? <><Image className="home-featured-backdrop" src={game.coverUrl} alt="" fill sizes="(min-width: 1280px) 900px, 70vw" aria-hidden="true" /><span className="home-featured-cover"><Image src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="190px" /></span></> : <><span className="home-featured-backdrop home-cover-placeholder" aria-hidden="true" /><span className="home-featured-cover home-cover-placeholder" role="img" aria-label={`${game.title} 暂无封面`}>RETROM</span></>}
        {sessionSave ? <div className="home-featured-save-preview"><Image src={sessionSave.screenshotUrl} alt={`${game.title} 上次存档截图`} fill sizes="310px" unoptimized /><span>将从这里继续</span></div> : null}
        <div className="home-featured-copy">
          <p className="home-featured-overline">{game.platform.name} · {game.platformInstance.name}</p>
          <h2>{game.title}</h2>
          <p className="home-featured-description">最近一次游玩记录</p>
          <div className="home-featured-facts"><span>上次游玩 <strong>{formatTime(game.lastPlayedAtMs)}</strong></span><span>累计游玩 <strong>{duration(game.activeDurationMs)}</strong></span><span>游玩 <strong>{game.sessionCount} 次</strong></span></div>
          <div className="home-featured-actions">
            <LaunchButton gameId={game.gameId} saveStateId={sessionSave?.saveStateId ?? null} returnTo="/" label={sessionSave ? "继续游玩" : "再玩一次"} />
            <span className="home-launch-note"><i aria-hidden="true" />本次将从{sessionSave ? "存档位置" : "游戏开头"}启动</span>
          </div>
        </div>
      </div>
      <div className="home-featured-bottom"><div><h3>{game.title}</h3><p>{sessionSave ? `上次存档保存于 ${formatTime(sessionSave.createdAtMs)}，可以直接回到当时的进度。` : "你最近玩过这款游戏；本次会使用当前运行配置从头开始。"}</p></div><Link href={`/games/${game.gameId}`}>查看游戏详情 →</Link></div>
    </div>
  </article>;
}

function QuickStart({ home }: { home: Home }) {
  const links = [
    { href: "/library", icon: "library" as const, title: "浏览游戏库", note: `查看全部 ${home.library.gameCount} 款游戏` },
    { href: "/recent", icon: "history" as const, title: "最近游玩", note: "回到最近玩过的游戏" },
    { href: "/saves", icon: "save" as const, title: "我的存档", note: `${home.library.saveStateCount} 份手动存档` },
  ];
  return <aside className="panel home-quick-panel">
    <div className="home-panel-head"><div><h2>快速开始</h2><p>不用翻列表，直接进入常用入口</p></div></div>
    <div className="home-quick-links">{links.map((item) => <Link className="home-quick-link" href={item.href} key={item.href}><span className="home-quick-icon"><AppIcon name={item.icon} /></span><span><strong>{item.title}</strong><small>{item.note}</small></span><b aria-hidden="true">›</b></Link>)}</div>
    <div className="home-quick-platforms"><div className="home-quick-platform-head"><strong>按平台浏览</strong><span>游玩次数最多</span></div><div className="home-quick-platform-grid">{home.quickPlatforms.map((platform) => <Link href={`/library?platformId=${encodeURIComponent(platform.id)}`} key={platform.id}><span><strong>{platform.name}</strong><small>{platform.gameCount} 款游戏</small></span><code>{platformCode(platform.id)}</code></Link>)}</div></div>
  </aside>;
}

export default async function HomePage() {
  const home = await backendJSON<Home>("/api/v1/home");
  return <div className="page-layout page-layout-home home-page">
    <PageHeader eyebrow="我的游戏" title="今天想玩什么？" description="回到最近玩的游戏，或者从资料库里找点经典游戏。" />

    <section className="home-layer home-first-layer" data-home-layer="1" aria-label="最近游玩与快速开始">
      <FeaturedGamePanel game={home.featuredGame} />
      <QuickStart home={home} />
    </section>

    <section className="home-layer home-recent-section" data-home-layer="2">
      <div className="home-section-head"><div><h2>最近游玩</h2><p>按最近一次游玩时间排列，最多展示 10 款游戏</p></div><Link href="/recent">查看全部</Link></div>
      {home.recentGames.length === 0 ? <div className="home-inline-empty">游玩过的游戏会出现在这里。</div> : <HorizontalRail className="home-recent-rail" label="最近游玩的游戏">
        {home.recentGames.map((game) => <Link className="home-recent-card" href={`/games/${game.gameId}`} key={game.gameId}>
          <span className="home-recent-cover">{game.coverUrl ? <Image src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="160px" /> : <span role="img" aria-label={`${game.title} 暂无封面`}>RETROM</span>}</span>
          <span className="home-recent-copy"><strong>{game.title}</strong><small>{game.platform.name} · {game.platformInstance.name}<br />{formatTime(game.lastPlayedAtMs)} 玩过</small><span><small>累计游玩</small><b>{duration(game.activeDurationMs)}</b></span></span>
        </Link>)}
      </HorizontalRail>}
    </section>

    <section className="home-layer home-platform-section" data-home-layer="3">
      <div className="home-section-head"><div><h2>换个平台逛逛</h2><p>点击图钉可把常用平台固定在最前面</p></div><Link href="/library">进入游戏库 →</Link></div>
      <PlatformRail platforms={home.platforms} />
    </section>

    <section className="home-layer home-summary" data-home-layer="4" aria-label="我的资料库">
      <strong>我的资料库</strong><span><b>{home.library.gameCount}</b> 款游戏</span><span><b>{home.library.saveStateCount}</b> 份存档</span><span><b>{duration(home.play.activeDurationMs)}</b> 累计游玩</span>
    </section>
  </div>;
}
