import Image from "next/image";
import Link from "next/link";
import { ButtonLink, EmptyState, Kpi, PageHeader, StatusBadge } from "@/components/ui";
import { LaunchButton } from "@/features/player/launch-button";
import { backendJSON, formatTime } from "@/lib/backend";

type Home = {
  library: { gameCount: number; saveStateCount: number };
  play: { activeDurationMs: number };
  recentGames: Array<{ gameId: string; title: string; platform: { name: string }; platformInstance: { name: string }; lastPlayedAtMs: number; activeDurationMs: number; coverUrl: string | null }>;
  recentSaves: Array<{ saveStateId: string; gameId: string; gameTitle: string; name: string; createdAtMs: number; activeDurationMs: number; screenshotUrl: string }>;
};

function duration(value: number) {
  const hours = value / 3_600_000;
  return hours < 1 ? `${Math.floor(value / 60_000)} 分钟` : `${hours.toFixed(hours < 10 ? 1 : 0)} 小时`;
}

export default async function HomePage() {
  const home = await backendJSON<Home>("/api/v1/home");
  return (
    <div className="page-layout page-layout-home">
      <PageHeader eyebrow="我的游戏" title="今天想玩什么？" description="从最近的进度继续，或重新发现资料库里的经典游戏。" />
      <section className="home-primary-grid">
        <aside className="panel continue-spotlight">
          <div className="panel-head"><div><p className="eyebrow">接着上次进度</p><h2>继续游戏</h2><p>从最近一份可用存档恢复</p></div><StatusBadge tone={home.recentSaves.length ? "good" : "neutral"}>{home.recentSaves.length ? "已准备好" : "暂无存档"}</StatusBadge></div>
          <div className="panel-body">
            {home.recentSaves[0] ? <div className="continue-card"><div className="save-shot"><Image src={home.recentSaves[0].screenshotUrl} alt={`${home.recentSaves[0].gameTitle} 存档画面`} width={640} height={360} unoptimized /><div className="save-shot-action"><LaunchButton gameId={home.recentSaves[0].gameId} saveStateId={home.recentSaves[0].saveStateId} returnTo="/" label="继续此存档" /></div></div><div className="continue-card-copy"><h2>{home.recentSaves[0].gameTitle}</h2><p><span>已游玩 {duration(home.recentSaves[0].activeDurationMs)}</span><span>保存于 {formatTime(home.recentSaves[0].createdAtMs)}</span></p></div></div> : <EmptyState title="还没有手动存档" description="游玩时保存进度后，可从首页一键继续。" action={<ButtonLink href="/library">选择一款游戏</ButtonLink>} />}
          </div>
        </aside>
        <div className="panel recent-panel">
          <div className="panel-head"><div><h2>最近游玩</h2><p>继续探索你的游戏资料库</p></div><Link className="row-action" href="/recent">查看全部</Link></div>
          <div className="panel-body">
            {home.recentGames.length === 0 ? <EmptyState title="还没有游玩记录" description="游戏开始后，最近玩过的内容会出现在这里。" action={<ButtonLink href="/library">浏览游戏库</ButtonLink>} /> : <div className="recent-game-grid">{home.recentGames.map((game) => <Link className="recent-game-card" href={`/games/${game.gameId}`} key={game.gameId}>{game.coverUrl ? <Image src={game.coverUrl} alt={`${game.title} 封面`} width={480} height={360} /> : <div className="recent-game-cover" role="img" aria-label={`${game.title} 暂无封面`} />}<div><StatusBadge tone="info">最近游玩</StatusBadge><h2>{game.title}</h2><p>{game.platform.name} · {game.platformInstance.name}<br />{formatTime(game.lastPlayedAtMs)}</p><div className="metric-line"><span>累计</span><strong>{duration(game.activeDurationMs)}</strong></div></div></Link>)}</div>}
          </div>
        </div>
      </section>
      <section className="kpi-grid home-metrics" aria-label="资料库概况">
        <Kpi label="有效游玩时长" value={duration(home.play.activeDurationMs)} note="只累计实际运行且可见的时间" tone="slate" />
        <Kpi label="游戏库" value={home.library.gameCount} note="已经整理并可浏览的游戏" />
        <Kpi label="手动存档" value={home.library.saveStateCount} note="随时回到保存时的进度" tone="cyan" />
      </section>
    </div>
  );
}
