"use client";

import Image from "next/image";
import Link from "next/link";
import { FavoriteActions } from "./favorite-actions";
import type { FavoriteFolder, FavoriteGame, FavoriteReference } from "./favorite-api";
import { TagChips } from "@/components/tag-picker";

function FavoritePoster({ game }: { game: FavoriteGame }) {
  if (game.coverUrl) return <Image src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="(min-width: 2600px) 280px, 270px" unoptimized />;
  return <span className="favorite-poster" role="img" aria-label={`${game.title} 暂无封面`}><small>RETROM CLASSICS</small><strong>{game.title}</strong><span>{game.defaultCore.name}</span></span>;
}

export function FavoriteGrid({
  games, folders, selecting, selected, busy = false, onToggle, onFavoriteChange,
}: {
  games: FavoriteGame[]; folders: FavoriteFolder[]; selecting: boolean; selected: ReadonlySet<string>; busy?: boolean;
  onToggle: (gameId: string) => void; onFavoriteChange: (gameId: string, favorite: FavoriteReference | null) => void;
}) {
  const folderNames = new Map(folders.map((folder) => [folder.folderId, folder.name]));
  return <div className={`favorite-game-grid ${selecting ? "is-selecting" : ""}`}>
    {games.map((game) => {
      const folderTags = game.favorite.folderIds.map((folderId) => folderNames.get(folderId)).filter((name): name is string => Boolean(name));
      return <article className="favorite-game-card" data-favorite-game={game.gameId} key={game.gameId}>
        <div className="favorite-game-cover">
          <Link href={`/games/${game.gameId}`} aria-label={`查看游戏“${game.title}”详情`}><FavoritePoster game={game} /><span>查看游戏详情 →</span></Link>
          {selecting ? <button
            className={`favorite-select ${selected.has(game.gameId) ? "is-selected" : ""}`}
            type="button"
            disabled={busy}
            aria-label={`${selected.has(game.gameId) ? "取消选择" : "选择"}游戏“${game.title}”`}
            aria-pressed={selected.has(game.gameId)}
            onClick={() => onToggle(game.gameId)}
          >{selected.has(game.gameId) ? "✓" : ""}</button> : null}
        </div>
        <div className="favorite-game-body">
          <Link href={`/games/${game.gameId}`}><h3>{game.title}</h3></Link>
          <p><span>{game.platform.name}</span><span>{game.releaseYear ?? "年份未知"}</span></p>
          <TagChips tags={game.tags ?? []} limit={2} label={`${game.title} 的游戏标签`} />
          <div className="favorite-tags" aria-label={`${game.title} 的收藏夹`}>{folderTags.slice(0, 2).map((name) => <span key={name}>{name}</span>)}{folderTags.length > 2 ? <span aria-label={`另外 ${folderTags.length - 2} 个收藏夹`}>+{folderTags.length - 2}</span> : null}</div>
        </div>
        <FavoriteActions
          gameId={game.gameId}
          title={game.title}
          initialFavorite={game.favorite}
          variant="favorite-card"
          onChange={(favorite) => onFavoriteChange(game.gameId, favorite)}
        />
      </article>;
    })}
  </div>;
}
