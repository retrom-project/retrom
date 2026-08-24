"use client";

import Image from "next/image";
import { useEffect, useMemo, useState } from "react";
import type { ImmersivePlatform } from "./api";
import styles from "./platform.module.css";

const COVER_ROTATION_MS = 3_000;
type FeaturedGame = ImmersivePlatform["featuredGames"][number];

function useCoverRotationEnabled() {
  const [enabled, setEnabled] = useState(false);

  useEffect(() => {
    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
    const update = () => setEnabled(!reducedMotion.matches && !document.hidden);
    update();
    reducedMotion.addEventListener("change", update);
    document.addEventListener("visibilitychange", update);
    return () => {
      reducedMotion.removeEventListener("change", update);
      document.removeEventListener("visibilitychange", update);
    };
  }, []);

  return enabled;
}

function coverSlot(index: number, frontIndex: number, count: number) {
  return (index - frontIndex + count) % count;
}

export function PlatformCoverStack({ games, platformName }: {
  games: readonly FeaturedGame[];
  platformName: string;
}) {
  const visibleGames = useMemo(() => games.slice(0, 3), [games]);
  const [frontIndex, setFrontIndex] = useState(0);
  const rotationEnabled = useCoverRotationEnabled();

  useEffect(() => {
    if (!rotationEnabled || visibleGames.length < 2) {return;}
    const timer = window.setInterval(() => {
      setFrontIndex((current) => (current + 1) % visibleGames.length);
    }, COVER_ROTATION_MS);
    return () => window.clearInterval(timer);
  }, [rotationEnabled, visibleGames.length]);

  if (!visibleGames.length) {return null;}
  return <div
    className={styles.coverStack}
    data-platform-cover-stack="true"
    aria-label={`${platformName} 最近游戏封面`}
  >
    {visibleGames.map((game, index) => {
      const slot = coverSlot(index, frontIndex, visibleGames.length);
      return <figure
        key={game.gameId}
        className={`${styles.coverStackItem} ${styles[`coverStackSlot${slot}`]}`}
        data-cover-slot={slot}
        data-game-id={game.gameId}
      >
        {game.coverUrl
          ? <Image
            src={game.coverUrl}
            alt={`${game.title} 封面`}
            fill
            sizes="(min-width: 1920px) 260px, 18vw"
            unoptimized
            draggable="false"
          />
          : <div className={styles.coverFallback} role="img" aria-label={`${game.title} 暂无封面`}>
            <span>RETROM</span><strong>{game.title}</strong>
          </div>}
      </figure>;
    })}
  </div>;
}
