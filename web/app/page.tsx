import Image from "next/image";
import Link from "next/link";
import { ButtonLink, EmptyState, Kpi, PageHeader, StatusBadge } from "@/components/ui";
import { LaunchButton } from "@/features/player/launch-button";
import { backendJSON, formatTime } from "@/lib/backend";

type Home = {
  library: { gameCount: number; saveStateCount: number };
  play: { activeDurationMs: number };
  recentGames: Array<{ gameId: string; title: string; platform: { name: string }; platformInstance: { name: string }; lastPlayedAtMs: number; activeDurationMs: number; coverUrl: string | null }>;
  recentSaves: Array<{ saveStateId: string; gameId: string; gameTitle: string; name: string; createdAtMs: number; screenshotUrl: string }>;
};

function duration(value: number) {
  const hours = value / 3_600_000;
  return hours < 1 ? `${Math.floor(value / 60_000)} 分钟` : `${hours.toFixed(hours < 10 ? 1 : 0)} 小时`;
}

export default async function HomePage() {
  const home = await backendJSON<Home>("/api/v1/home");
  return (
    <>
      <PageHeader eyebrow="我的游戏" title="今天想玩什么？" description="从最近的进度继续，或重新发现资料库里的经典游戏。" actions={<ButtonLink href="/library">浏览游戏库</ButtonLink>} />
      <section className="kpi-grid" aria-label="资料库概况">
        <Kpi label="有效游玩时长" value={duration(home.play.activeDurationMs)} note="只累计实际运行且可见的时间" tone="slate" />
        <Kpi label="已收藏游戏" value={home.library.gameCount} note="已发布且可在游戏库浏览" />
        <Kpi label="手动存档" value={home.library.saveStateCount} note="随时回到保存时的进度" tone="cyan" />
      </section>
      <section className="split-grid">
        <div className="panel">
          <div className="panel-head"><div><h2>最近游玩</h2><p>继续探索你的游戏资料库</p></div><Link className="row-action" href="/library">查看全部</Link></div>
          <div className="panel-body">
            {home.recentGames.length === 0 ? <EmptyState title="还没有游玩记录" description="游戏开始后，最近玩过的内容会出现在这里。" action={<ButtonLink href="/library">浏览游戏库</ButtonLink>} /> : <div className="recent-game-grid">{home.recentGames.map((game) => <Link className="recent-game-card" href={`/games/${game.gameId}`} key={game.gameId}>{game.coverUrl ? <Image src={game.coverUrl} alt={`${game.title} 封面`} width={480} height={360} /> : <div className="recent-game-cover" role="img" aria-label={`${game.title} 暂无封面`} />}<div><StatusBadge tone="good">最近游玩</StatusBadge><h2>{game.title}</h2><p>{game.platform.name} · {game.platformInstance.name}<br />{formatTime(game.lastPlayedAtMs)}</p><div className="metric-line"><span>累计</span><strong>{duration(game.activeDurationMs)}</strong></div></div></Link>)}</div>}
          </div>
        </div>
        <aside className="panel">
          <div className="panel-head"><div><h2>继续游戏</h2><p>从最近一份兼容存档恢复</p></div><StatusBadge tone={home.recentSaves.length ? "good" : "neutral"}>{home.recentSaves.length ? "可继续" : "暂无存档"}</StatusBadge></div>
          <div className="panel-body">
            {home.recentSaves[0] ? <div className="continue-card"><Image src={home.recentSaves[0].screenshotUrl} alt={`${home.recentSaves[0].gameTitle} 存档画面`} width={640} height={360} unoptimized /><div><h2>{home.recentSaves[0].gameTitle}</h2><p className="game-meta">{home.recentSaves[0].name} · {formatTime(home.recentSaves[0].createdAtMs)}</p><LaunchButton gameId={home.recentSaves[0].gameId} saveStateId={home.recentSaves[0].saveStateId} returnTo="/" label="继续此存档" /><p><Link className="row-action" href="/saves">查看全部存档</Link></p></div></div> : <EmptyState title="还没有手动存档" description="游玩时保存进度后，可从首页一键继续。" />}
          </div>
        </aside>
      </section>
    </>
  );
}
