import Image from "next/image";
import Link from "next/link";
import type { LatestGame, RecentGame } from "./home-data";
import { HomeTime } from "./home-time";

export function HomeGameCard({ game }: { game: RecentGame | LatestGame }) {
  return <Link className="home-recent-card" href={`/games/${game.gameId}`}>
    <span className="home-recent-cover">{game.coverUrl
      ? <Image src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="(min-width: 1800px) 240px, 180px" unoptimized />
      : <span className="home-poster-placeholder"><small>RETROM CLASSICS</small><span>{game.title}</span></span>}
      <span className="home-poster-platform"><span>{game.platform.name}</span></span>
    </span>
    <span className="home-recent-copy"><strong title={game.title}>{game.title}</strong><small>{"lastPlayedAtMs" in game ? <><HomeTime value={game.lastPlayedAtMs} /> 玩过</> : game.platform.name}</small></span>
  </Link>;
}
