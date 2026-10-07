"use client";
import Link from "next/link";
import { useCallback } from "react";
import { useResource } from "@/lib/use-resource";
import { loadGames } from "@/features/library/api";
import type { Destination } from "./immersive-data";
import { PlatformCoverStack } from "./platform-cover-stack";
import styles from "./immersive.module.css";
import platform from "./platform.module.css";

export function PlatformCarousel({ destinations, selected, onSelect, onOpen }: {
  destinations: Destination[];
  selected: number;
  onSelect: (index: number) => void;
  onOpen: () => void;
}) {
  const current = destinations[selected];
  const preview = useResource(useCallback(async () => {
    if (!current?.directoryId) { return current?.featured ?? []; }
    const page = await loadGames("library", { q: "", offset: 0, limit: 3, platformInstanceId: current.directoryId });
    return page.items;
  }, [current]));
  if (!current) { return <div className={styles.centerState}><h1>还没有可游玩的游戏</h1><p>返回普通界面导入并发布游戏后再来看看。</p><Link className="button" href="/">返回普通首页</Link></div>; }
  return (
    <section className={platform.platformView} aria-label="游戏平台">
      <header className={styles.viewHeading}><p>选择游戏平台</p><h1>今天想玩哪个平台？</h1></header>
      <div className={platform.platformStage}>
        <div className={platform.platformCarousel}>
          {[-1, 0, 1].map((shift) => {
            const index = (selected + shift + destinations.length) % destinations.length;
            const destination = destinations[index];
            return (
              <button key={shift} aria-label={`${destination.name}，${destination.count} ${destination.view === "saves" ? "份存档" : "款游戏"}`}
                className={`${platform.platformCard} ${shift === 0 ? platform.currentPlatform : ""} ${platform[`platformTone${index % 5}`]}`}
                onClick={() => shift === 0 ? onOpen() : onSelect(index)}>
                <div className={platform.platformCopy}>
                  <span className={platform.platformCode}>{destination.code.slice(0, 4)}</span>
                  <p>{shift === 0 ? destination.directoryId ? "当前平台" : "当前入口" : "相邻入口"}</p>
                  <h2>{destination.name}</h2>
                  <strong>{destination.count} {destination.view === "saves" ? "份存档" : "款游戏"}</strong>
                  <small>按 A 或回车浏览{destination.view === "saves" ? "存档" : "游戏"}</small>
                </div>
                {shift === 0 ? <PlatformCoverStack key={destination.id} games={preview.data ?? []} platformName={destination.name} /> : null}
              </button>
            );
          })}
        </div>
        <div className={platform.platformPosition} aria-label={`第 ${selected + 1} 个入口，共 ${destinations.length} 个`}>
          <span>{String(selected + 1).padStart(2, "0")}</span>
          <div className={platform.platformPositionTrack}><i style={{ width: `${100 / destinations.length}%`, transform: `translateX(${selected * 100}%)` }} /></div>
          <span>{String(destinations.length).padStart(2, "0")}</span>
        </div>
      </div>
    </section>
  );
}
