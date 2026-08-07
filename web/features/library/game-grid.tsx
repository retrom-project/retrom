import Image from "next/image";
import Link from "next/link";
import { AppIcon } from "@/components/app-icon";
import { ButtonLink, EmptyState } from "@/components/ui";

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
    <div className="admin-game-grid">
      {games.map((game) => (
        <Link className="admin-game-card" href={`/games/${game.gameId}`} key={game.gameId}>
          <div className="admin-game-cover">
            {game.coverUrl ? <Image src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="(min-width: 2600px) 300px, 260px" /> : <span role="img" aria-label={`${game.title} 暂无封面`}>RETROM</span>}
            {game.status !== "PUBLISHED" ? <span className="admin-game-hidden" title="游戏当前不可见" aria-label="游戏当前不可见"><AppIcon name="eye-off" /></span> : null}
          </div>
          <div className="admin-game-body"><h2>{game.title}</h2><p>{game.platform.name}</p><small>{game.platformInstance.name}</small></div>
        </Link>
      ))}
    </div>
  );
}
