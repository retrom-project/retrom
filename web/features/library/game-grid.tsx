"use client";

import Image from "next/image";
import Link from "next/link";
import { useEffect, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { ButtonLink, EmptyState } from "@/components/ui";
import { formatLibraryPlayedAt, type GameSummary } from "./game-library";

export type { GameSummary } from "./game-library";

function GamePoster({ game }: { game: GameSummary }) {
  if (game.coverUrl) return <Image src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="(min-width: 2600px) 280px, 270px" />;
  return <span className="library-poster" role="img" aria-label={`${game.title} 暂无封面`}><small>RETROM CLASSICS</small><strong>{game.title}</strong><span>{game.defaultCore.name}</span></span>;
}

export function GameGrid({ games, nowMs, filtered = false }: { games: GameSummary[]; nowMs: number; filtered?: boolean }) {
  const [menuId, setMenuId] = useState<string | null>(null);
  useEffect(() => {
    if (!menuId) return;
    const closeOutside = (event: PointerEvent) => {
      const card = event.target instanceof Element ? event.target.closest("[data-library-game]") : null;
      if (card?.getAttribute("data-library-game") !== menuId) setMenuId(null);
    };
    const closeEscape = (event: KeyboardEvent) => { if (event.key === "Escape") setMenuId(null); };
    document.addEventListener("pointerdown", closeOutside);
    document.addEventListener("keydown", closeEscape);
    return () => {
      document.removeEventListener("pointerdown", closeOutside);
      document.removeEventListener("keydown", closeEscape);
    };
  }, [menuId]);

  if (games.length === 0 && filtered) return <EmptyState title="没有找到游戏" description="当前搜索、平台或游戏集合没有匹配项，请调整条件后重试。" action={<ButtonLink href="/library" secondary>清除筛选</ButtonLink>} />;
  if (games.length === 0) return <EmptyState title="游戏库还是空的" description="从管理后台选择游戏文件或目录，完成验证与审核后即可在这里游玩。" />;
  return <div className="library-game-grid">
    {games.map((game) => <article className="library-game-card" data-library-game={game.gameId} key={game.gameId}>
      <div className="library-game-cover">
        <Link href={`/games/${game.gameId}`} aria-label={`查看${game.title}游戏详情`}><GamePoster game={game} /><span className="library-platform-tag">{game.platform.name}</span><span className="library-card-hover"><strong>查看游戏详情 →</strong></span></Link>
        {game.status !== "PUBLISHED" ? <span className="library-game-hidden" title="游戏当前不可见" aria-label="游戏当前不可见"><AppIcon name="eye-off" /></span> : null}
      </div>
      <div className="library-game-body">
        <div className="library-game-title-row"><Link href={`/games/${game.gameId}`}><h2>{game.title}</h2></Link><button type="button" aria-label={`游戏“${game.title}”的更多操作`} aria-haspopup="menu" aria-expanded={menuId === game.gameId} onClick={() => setMenuId((current) => current === game.gameId ? null : game.gameId)}>•••</button>{menuId === game.gameId ? <div className="library-game-menu" role="menu"><Link role="menuitem" href={`/games/${game.gameId}`}>查看游戏详情</Link><Link role="menuitem" href={`/saves?gameId=${encodeURIComponent(game.gameId)}`}>查看相关存档</Link></div> : null}</div>
        <p><span>{game.platform.name}</span><span>{game.platformInstance.name}</span></p>
        <div className="library-game-played"><span>最近游玩</span><strong>{formatLibraryPlayedAt(game.lastPlayedAtMs, nowMs)}</strong></div>
      </div>
    </article>)}
  </div>;
}
