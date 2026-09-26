import Image from "next/image";
import Link from "next/link";
import { formatTime } from "@/lib/backend";
import type { LatestGame, RecentGame } from "./home-data";

export function HomeGameCard({ game }: { game: RecentGame | LatestGame }) {
  return <Link className="home-recent-card" href={`/games/${game.gameId}`}>
    <span className="home-recent-cover">{game.coverUrl
      ? <Image src={game.coverUrl} alt={`${game.title} 封面`} fill sizes="(min-width: 1800px) 240px, 180px" unoptimized />
      : <span className="home-poster-placeholder"><small>RETROM CLASSICS</small><span>{game.title}</span></span>}
      <span className="home-poster-platform">{game.platform.name}</span>
    </span>
    <span className="home-recent-copy"><strong title={game.title}>{game.title}</strong><small>{"lastPlayedAtMs" in game ? `${formatTime(game.lastPlayedAtMs)} 玩过` : game.platform.name}</small></span>
  </Link>;
}
