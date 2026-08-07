import Image from "next/image";
import Link from "next/link";
import { ButtonLink, EmptyState, StatusBadge } from "@/components/ui";
import { statusTone } from "@/lib/status";

export type GameSummary = {
  gameId: string;
  title: string;
  platform: { id: string; name: string };
  platformInstance: { id: string; name: string };
  status: string;
  coverUrl: string | null;
};

export function GameGrid({ games, filtered = false }: { games: GameSummary[]; filtered?: boolean }) {
  if (games.length === 0 && filtered) return <EmptyState title="没有找到游戏" description="当前搜索或平台条件没有匹配项，请调整条件后重试。" action={<ButtonLink href="/library" secondary>清除筛选</ButtonLink>} />;
  if (games.length === 0) return <EmptyState title="游戏库还是空的" description="从管理后台选择游戏文件或目录，完成验证与审核后即可在这里游玩。" />;
  return (
    <div className="game-grid">
      {games.map((game) => (
        <Link className="game-card" href={`/games/${game.gameId}`} key={game.gameId}>
          <div className={`cover${game.coverUrl ? " has-image" : ""}`}>
            {game.coverUrl ? <Image className="cover-image" src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="(min-width: 3000px) 12.5vw, (min-width: 1920px) 16.67vw, 25vw" /> : null}
            {game.status !== "PUBLISHED" ? <StatusBadge tone={statusTone(game.status)}>{game.status === "DELETED" ? "已删除" : "需要处理"}</StatusBadge> : null}
          </div>
          <div className="game-card-body"><h2>{game.title}</h2><div className="game-meta"><span>{game.platform.name}</span><span>{game.platformInstance.name}</span></div></div>
        </Link>
      ))}
    </div>
  );
}
