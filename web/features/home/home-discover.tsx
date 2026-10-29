"use client";
import Image from "next/image";
import Link from "next/link";
import { ResourceState } from "@/components/resource-state";
import type { Game } from "@/lib/api/types";
import { useResource } from "@/lib/use-resource";
import { loadDiscover } from "./home-data";
import { HomeGameRail } from "./home-sections";

function loadPhoneDiscover(signal?: AbortSignal) { return loadDiscover(6, signal); }
function loadDesktopDiscover(signal?: AbortSignal) { return loadDiscover(12, signal); }

export function HomeDiscover({ phone = false }: { phone?: boolean }) {
  const discover = useResource(phone ? loadPhoneDiscover : loadDesktopDiscover);
  return (
    <section className={phone ? undefined : "home-layer"} aria-label="发现游戏">
      <div className={phone ? "phone-section-head" : "home-section-head"}>
        <h2>发现游戏</h2>
        <Link href="/library">查看全部</Link>
      </div>
      <ResourceState resource={discover}>
        {({ items }) => items.length ? (
          phone ? <div className="phone-game-grid">
            {items.map((game) => <PhoneGame key={game.id} game={game} />)}
          </div> : <HomeGameRail items={items.map((game) => ({ game, caption: game.directoryName }))} />
        ) : <div className="home-inline-empty">游戏库中还没有可展示的游戏。</div>}
      </ResourceState>
    </section>
  );
}

function PhoneGame({ game }: { game: Game }) {
  const cover = game.media.find((item) => item.kind === "cover");
  return (
    <Link className="phone-game-card" href={`/games/${game.id}`}>
      <div className="phone-game-poster">
        {cover ? <Image src={cover.url} fill unoptimized sizes="45vw" alt="" /> : <span>{game.title}</span>}
      </div>
      <strong>{game.title}</strong>
      <small>{game.directoryName}</small>
    </Link>
  );
}
