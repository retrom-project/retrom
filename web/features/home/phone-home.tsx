"use client";
import Image from "next/image";
import Link from "next/link";
import { useState } from "react";
import { useResource } from "@/lib/use-resource";
import { loadGames } from "@/features/library/api";
import { HomeFeatured } from "./home-sections";
import { ResourceState } from "@/components/resource-state";
import type { Schema } from "@/lib/api/types";
import { AppIcon } from "@/components/app-icon";
async function loadDiscover() {
  return loadGames("library", { q: "", offset: 0, limit: 6 });
}
export function PhoneHome({ data }: { data: Schema<"Home"> }) {
  const [query, setQuery] = useState("");
  const discover = useResource(loadDiscover);
  return (
    <div className="phone-home">
      <form className="phone-home-search" action="/library">
        <AppIcon name="search" />
        <input
          name="q"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="想玩什么？搜一下"
          aria-label="搜索游戏"
        />
        <button>搜索</button>
      </form>
      <section className="phone-continue">
        <h1>今天，玩点什么？</h1>
        <div className="phone-continue-card">
          <HomeFeatured data={data} />
        </div>
      </section>
      <section>
        <div className="phone-section-head">
          <h2>发现游戏</h2>
          <Link href="/library">查看全部</Link>
        </div>
        <ResourceState resource={discover}>
          {(games) => (
            <div className="phone-game-grid">
              {games.items.map((game) => (
                <PhoneGame key={game.id} game={game} />
              ))}
            </div>
          )}
        </ResourceState>
      </section>
    </div>
  );
}
function PhoneGame({ game }: { game: Schema<"Game"> }) {
  const cover = game.media.find((item) => item.kind === "cover");
  return (
    <Link className="phone-game-card" href={`/games/${game.id}`}>
      <div className="phone-game-poster">
        {cover ? (
          <Image src={cover.url} fill unoptimized sizes="45vw" alt="" />
        ) : (
          <span>{game.title}</span>
        )}
      </div>
      <strong>{game.title}</strong>
      <small>{game.directoryName}</small>
    </Link>
  );
}
