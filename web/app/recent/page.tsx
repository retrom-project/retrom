import Image from "next/image";
import Link from "next/link";
import { EmptyState, PageHeader } from "@/components/ui";
import { backendJSON, formatTime } from "@/lib/backend";

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

function duration(value: number) {
  const hours = value / 3_600_000;
  if (hours >= 1) return `${hours.toFixed(hours < 10 ? 1 : 0)} 小时`;
  if (value < 60_000) return "少于 1 分钟";
  return `${Math.floor(value / 60_000)} 分钟`;
}

export default async function RecentGamesPage() {
  const response = await backendJSON<{ items: RecentGame[] }>("/api/v1/recent-games?limit=50");
  return <div className="page-layout">
    <PageHeader eyebrow="我的游戏" title="最近游玩" description="按最近一次游玩时间排列，快速回到你刚刚放下的游戏。" />
    {response.items.length === 0 ? <EmptyState title="还没有游玩记录" description="从游戏库启动一次游戏后，这里会显示最近玩过的内容。" /> : <section className="panel recent-history" aria-label="最近游玩列表">
      {response.items.map((game) => <Link className="recent-history-row" href={`/games/${game.gameId}`} key={game.gameId}>
        <div className="recent-history-cover">{game.coverUrl ? <Image src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="96px" /> : <span role="img" aria-label={`${game.title} 暂无封面`}>RETROM</span>}</div>
        <div className="recent-history-main"><h2>{game.title}</h2><p>{game.platform.name} · {game.platformInstance.name}</p></div>
        <div className="recent-history-fact"><span>最近游玩</span><strong>{formatTime(game.lastPlayedAtMs)}</strong></div>
        <div className="recent-history-fact"><span>累计时长</span><strong>{duration(game.activeDurationMs)}</strong></div>
        <div className="recent-history-fact"><span>游玩次数</span><strong>{game.sessionCount} 次</strong></div>
      </Link>)}
    </section>}
  </div>;
}
