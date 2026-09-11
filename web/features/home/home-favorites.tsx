"use client";

import Image from "next/image";
import Link from "next/link";
import { useEffect, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { useAuth } from "@/features/auth/auth-provider";
import { loadFavorites, type AuthenticatedFetch, type FavoriteGame } from "@/features/favorites/favorite-api";

export async function loadHomeFavorites(fetcher: AuthenticatedFetch, signal: AbortSignal) {
  const games: FavoriteGame[] = [];
  const cursors = new Set<string>();
  let cursor = "";
  do {
    const query = new URLSearchParams({ scope: "ALL", sort: "FAVORITED_DESC", limit: "3" });
    if (cursor) {query.set("cursor", cursor);}
    const { data } = await loadFavorites(fetcher, query.toString(), signal);
    for (const game of data.items) {
      if (game.availability === "PUBLISHED" && !games.some((item) => item.gameId === game.gameId)) {games.push(game);}
      if (games.length === 3) {return games;}
    }
    cursor = data.nextCursor ?? "";
    if (cursor && cursors.has(cursor)) {throw new Error("Repeated favorite cursor");}
    cursors.add(cursor);
  } while (cursor && !signal.aborted);
  return games;
}

type Result = { userId: string; games: FavoriteGame[]; error: boolean };

export function HomeFavorites() {
  const { context, authenticatedFetch } = useAuth();
  const userId = context.user?.userId;
  const [result, setResult] = useState<Result | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!userId) {return;}
    const controller = new AbortController();
    void loadHomeFavorites(authenticatedFetch, controller.signal).then((games) => {
      if (!controller.signal.aborted) {setResult({ userId, games, error: false });}
    }).catch(() => {
      if (!controller.signal.aborted) {setResult({ userId, games: [], error: true });}
    });
    return () => controller.abort();
  }, [authenticatedFetch, userId, attempt]);

  if (!userId) {return null;}
  const current = result?.userId === userId ? result : null;
  return <section className="home-favorites" aria-label="收藏的游戏">
    <div className="home-favorites-head"><h3>收藏的游戏</h3>{current && !current.error && current.games.length > 0 ? <Link href="/favorites">查看全部</Link> : null}</div>
    <div className="home-favorites-body">{!current ? <p className="home-favorites-message" role="status">正在读取收藏…</p> : current.error ? <div className="home-favorites-empty"><p role="alert">暂时无法读取收藏</p><button className="button secondary" type="button" onClick={() => {setResult(null); setAttempt((value) => value + 1);}}>重新加载</button></div> : current.games.length === 0 ? <div className="home-favorites-empty"><AppIcon name="heart" /><div><strong>把喜欢的游戏留在这里</strong><p>在游戏卡片上点亮爱心，下次从这里出发。</p><Link href="/library">浏览游戏库</Link></div></div> : <div className="home-favorites-list">{current.games.map((game) => <Link className="home-favorite-game" aria-label={`${game.title} · ${game.platform.name}`} href={`/games/${game.gameId}`} key={game.gameId}>
      <span className="home-favorite-cover">{game.coverUrl ? <Image src={game.coverUrl} alt="" fill sizes="50px" unoptimized /> : <span aria-hidden="true">R</span>}</span>
      <span className="home-favorite-copy"><strong>{game.title}</strong><small>{game.platform.name}</small></span>
    </Link>)}</div>}</div>
  </section>;
}
