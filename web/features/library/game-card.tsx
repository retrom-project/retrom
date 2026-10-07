"use client";
import { useToast } from "@/components/toast-provider";
import Image from "next/image";
import Link from "next/link";
import { useMemo, useState } from "react";
import type { Game } from "@/lib/api/types";
import { AppIcon } from "@/components/app-icon";
import { BrowserTime } from "@/components/browser-time";
import { FavoriteCollectionDialog } from "@/features/favorites/favorite-collection-dialog";
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
  const [organizing, setOrganizing] = useState(false);
  const ids = useMemo(() => [game.id], [game.id]);
  const { notify } = useToast();
  const [busy, setBusy] = useState(false);
  const target = href ?? `/games/${game.id}`;
  const cover = game.media.find((media) => media.kind === "cover");
  async function favorite() {
    setBusy(true);
    try {
      await toggleFavorite(game.id, !game.favorite);
      notify({ tone: "good", message: game.favorite ? "已取消收藏" : "已收藏游戏" });
      onChange?.();
    } catch (failure) {
      notify({ tone: "bad", message: failure instanceof Error ? failure.message : "操作失败，请重试。" });
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
              <small>RETROM CLASSICS</small>
              <strong>{game.title}</strong>
              <span>{game.platformId}</span>
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
          <button aria-label={`整理${game.title}的收藏夹`} onClick={() => setOrganizing(true)}><AppIcon name="more" /></button>
        </div>
        <p>
          <span>{game.directoryName}</span>
          <span>{game.releaseYear || "年份未知"}</span>
        </p>
        <div className="library-game-played"><span>最近游玩</span><strong><BrowserTime value={game.lastPlayedAtMs} format="compact" /></strong></div>
      </div>
      {organizing ? <FavoriteCollectionDialog ids={ids} onClose={() => setOrganizing(false)} onSaved={() => { setOrganizing(false); onChange?.(); }} /> : null}
    </article>
  );
}
