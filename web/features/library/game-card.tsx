"use client";
import Image from "next/image";
import Link from "next/link";
import { useState } from "react";
import type { Game } from "@/lib/api/types";
import { AppIcon } from "@/components/app-icon";
import { toggleFavorite } from "./api";
export function GameCard({
  game,
  href,
  onChange,
}: {
  game: Game;
  href?: string;
  onChange?: () => void;
}) {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const target = href ?? `/games/${game.id}`;
  const cover = game.media.find((media) => media.kind === "cover");
  async function favorite() {
    setBusy(true);
    try {
      await toggleFavorite(game.id, !game.favorite);
      onChange?.();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "操作失败。");
    } finally {
      setBusy(false);
    }
  }
  return (
    <article className="library-game-card">
      <div className="library-game-cover">
        <Link href={target} aria-label={`打开${game.title}`}>
          {cover ? (
            <Image
              src={cover.url}
              alt=""
              fill
              sizes="(max-width: 767px) 45vw, 240px"
              unoptimized
            />
          ) : (
            <div className="library-poster">
              <small>{game.platformId}</small>
              <strong>{game.title}</strong>
              <span>RETROM</span>
            </div>
          )}
          <span className="library-platform-tag">{game.platformId}</span>
          <div className="library-card-hover">
            <strong>查看游戏</strong>
          </div>
        </Link>
        <div className="favorite-actions favorite-actions-card">
          {game.status === "published" ? (
            <button
              className={`favorite-heart${game.favorite ? " is-favorite" : ""}`}
              disabled={busy}
              aria-label={game.favorite ? "取消收藏" : "收藏游戏"}
              aria-pressed={game.favorite}
              onClick={() => void favorite()}
            >
              <AppIcon name="heart" />
            </button>
          ) : null}
        </div>
      </div>
      <div className="library-game-body">
        <div className="library-game-title-row">
          <Link href={target}>
            <h2 title={game.title}>{game.title}</h2>
          </Link>
        </div>
        <p>
          <span>{game.directoryName}</span>
          <span>{game.releaseYear || "年份未知"}</span>
        </p>
        <div className="workspace-tags">
          {game.tags.map((tag) => (
            <span className="status neutral" key={tag.id}>
              {tag.name}
            </span>
          ))}
        </div>
        {error ? <p role="alert">{error}</p> : null}
      </div>
    </article>
  );
}
