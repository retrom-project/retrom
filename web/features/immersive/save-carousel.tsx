"use client";

import Image from "next/image";
import { useEffect, useRef } from "react";
import type { ImmersiveGame } from "./api";
import styles from "./library.module.css";

type ImmersiveSave = ImmersiveGame["saveStates"][number];

function formatSaveTime(value: number) {
  return new Intl.DateTimeFormat("zh-CN", {
    year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false,
  }).format(value);
}

export function ImmersiveSaveCarousel({ gameTitle, onSelect, saves, selectedIndex }: {
  gameTitle: string;
  onSelect: (saveStateId: string) => void;
  saves: readonly ImmersiveSave[];
  selectedIndex: number;
}) {
  const selectedRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    selectedRef.current?.scrollIntoView({ behavior: "smooth", block: "nearest", inline: "center" });
  }, [selectedIndex]);

  return <section className={styles.saveDetails}>
    <header className={styles.saveHeading}>
      <p>我的存档 <strong>{selectedIndex + 1} / {saves.length}</strong></p>
      <span>左右切换 · A 从这里继续</span>
    </header>
    <div className={styles.saveRail} role="list" aria-label={`${gameTitle} 的存档`}>
      {saves.map((save, index) => {
        const selected = index === selectedIndex;
        const time = formatSaveTime(save.createdAtMs);
        return <button
          ref={selected ? selectedRef : undefined}
          className={`${styles.saveCard} ${selected ? styles.selectedSaveCard : ""}`.trim()}
          type="button"
          aria-label={`第 ${index + 1} 份存档，${time}`}
          aria-pressed={selected}
          tabIndex={selected ? 0 : -1}
          key={save.saveStateId}
          onClick={() => onSelect(save.saveStateId)}
        >
          <span className={styles.saveScreenshot}>
            {save.screenshotUrl ? <Image
              src={save.screenshotUrl}
              alt={`第 ${index + 1} 份存档截图`}
              fill
              sizes={selected ? "42vw" : "20vw"}
              loading={selected ? "eager" : "lazy"}
              unoptimized
            /> : <span>未提供截图</span>}
          </span>
          <time dateTime={new Date(save.createdAtMs).toISOString()}>{time}</time>
        </button>;
      })}
    </div>
  </section>;
}
