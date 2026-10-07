"use client";
import Image from "next/image";
import Link from "next/link";
import type { Game } from "@/lib/api/types";
import { AppIcon } from "@/components/app-icon";
export function FavoriteCard({
  game,
  selecting,
  selected,
  onSelect,
  onOrganize,
  onRemove,
}: {
  game: Game;
  selecting: boolean;
  selected: boolean;
  onSelect: () => void;
  onOrganize: () => void;
  onRemove: () => void;
}) {
  const cover = game.media.find((media) => media.kind === "cover");
  return (
    <article className="favorite-game-card">
      <div className="favorite-game-cover">
        <Link href={`/games/${game.id}`} aria-label={`打开${game.title}`}>
          {cover ? (
            <Image
              src={cover.url}
              alt=""
              fill
              unoptimized
              sizes="(max-width: 767px) 45vw, 280px"
            />
          ) : (
            <div className="favorite-poster">
              <small>{game.platformId}</small>
              <strong>{game.title}</strong>
              <span>RETROM</span>
            </div>
          )}
          <span>查看游戏</span>
        </Link>
        {selecting ? (
          <button
            className={`favorite-select${selected ? " is-selected" : ""}`}
            aria-label={`选择${game.title}`}
            aria-pressed={selected}
            onClick={onSelect}
          >
            {selected ? "✓" : "○"}
          </button>
        ) : (
          <div className="favorite-actions-favorite-card">
            <button
              className="favorite-heart is-favorite"
              aria-label={`取消收藏${game.title}`}
              onClick={onRemove}
            >
              <AppIcon name="heart" />
            </button>
            <button
              className="favorite-manage"
              aria-label={`整理${game.title}的收藏夹`}
              onClick={onOrganize}
            >
              •••
            </button>
          </div>
        )}
      </div>
      <div className="favorite-game-body">
        <Link href={`/games/${game.id}`}>
          <h3>{game.title}</h3>
        </Link>
        <p>
          <span>{game.directoryName}</span>
          <span>{game.releaseYear ?? "年份未知"}</span>
        </p>
      </div>
    </article>
  );
}
