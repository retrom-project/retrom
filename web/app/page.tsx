import Link from "next/link";
import { ButtonLink, EmptyState, Kpi, PageHeader, StatusBadge } from "@/components/ui";
import { LaunchButton } from "@/features/player/launch-button";
import { backendJSON, formatTime } from "@/lib/backend";

type Home = {
  library: { gameCount: number; saveStateCount: number };
  imports: { reviewPendingCount: number };
  play: { activeDurationMs: number };
  recentGames: Array<{ gameId: string; title: string; platform: { name: string }; platformInstance: { name: string }; lastPlayedAtMs: number; activeDurationMs: number }>;
  recentSaves: Array<{ saveStateId: string; gameId: string; gameTitle: string; name: string; createdAtMs: number }>;
};

function duration(value: number) {
  const hours = value / 3_600_000;
  return hours < 1 ? `${Math.floor(value / 60_000)} 分钟` : `${hours.toFixed(hours < 10 ? 1 : 0)} 小时`;
}

export default async function HomePage() {
  const home = await backendJSON<Home>("/api/v1/home");
  return (
    <>
      <PageHeader eyebrow="Welcome back" title="晚上好，本地玩家" description="从上次离开的地方继续，或浏览你的复古游戏收藏。" actions={<ButtonLink href="/library">浏览游戏库</ButtonLink>} />
      <section className="kpi-grid" aria-label="资料库概况">
        <Kpi label="有效游玩时长" value={duration(home.play.activeDurationMs)} note="只累计实际运行且可见的时间" tone="slate" />
        <Kpi label="已收藏游戏" value={home.library.gameCount} note="已发布且可在游戏库浏览" />
        <Kpi label="手动存档" value={home.library.saveStateCount} note="保留核心与运行版本快照" tone="cyan" />
        <Kpi label="待审核" value={home.imports.reviewPendingCount} note="入库任务等待你的决定" tone="amber" />
      </section>
      <section className="split-grid">
        <div className="panel">
          <div className="panel-head"><div><h2>最近游玩</h2><p>继续探索你的游戏资料库</p></div><Link className="row-action" href="/library">查看全部</Link></div>
          <div className="panel-body">
            {home.recentGames.length === 0 ? <EmptyState title="还没有游玩记录" description="导入并发布游戏后，这里会显示最近游玩的内容。" action={<ButtonLink href="/admin/imports/new">导入第一款游戏</ButtonLink>} /> : <div className="admin-grid">{home.recentGames.map((game) => <Link className="admin-card" href={`/games/${game.gameId}`} key={game.gameId}><StatusBadge tone="good">最近游玩</StatusBadge><h2>{game.title}</h2><p>{game.platform.name} · {game.platformInstance.name}<br />{formatTime(game.lastPlayedAtMs)}</p><div className="metric-line"><span>累计</span><strong>{duration(game.activeDurationMs)}</strong></div></Link>)}</div>}
          </div>
        </div>
        <aside className="panel">
          <div className="panel-head"><div><h2>继续游戏</h2><p>从最近一份兼容存档恢复</p></div><StatusBadge tone={home.recentSaves.length ? "good" : "neutral"}>{home.recentSaves.length ? "可继续" : "暂无存档"}</StatusBadge></div>
          <div className="panel-body">
            {home.recentSaves[0] ? <><h2>{home.recentSaves[0].gameTitle}</h2><p className="game-meta">{home.recentSaves[0].name} · {formatTime(home.recentSaves[0].createdAtMs)}</p><LaunchButton gameId={home.recentSaves[0].gameId} saveStateId={home.recentSaves[0].saveStateId} returnTo="/" label="继续此存档" /><p><Link className="row-action" href="/saves">查看全部存档</Link></p></> : <EmptyState title="还没有手动存档" description="游玩时保存进度后，可从首页一键继续。" />}
          </div>
        </aside>
      </section>
    </>
  );
}
