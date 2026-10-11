"use client";
import Image from "next/image";
import { useEffect, useRef } from "react";
import { BrowserTime } from "@/components/browser-time";
import { LaunchButton } from "@/features/player/launch-button";
import { formatBytes } from "@/lib/format-bytes";
import type { ImmersiveSaveSelection } from "./use-save-selection";
import { SaveRestoreWarning } from "./save-restore-warning";
import styles from "./library.module.css";

export function ImmersiveSaveCarousel({ gameTitle, selection, returnTo }: {
  gameTitle: string;
  selection: ImmersiveSaveSelection;
  returnTo: string;
}) {
  const selectedRef = useRef<HTMLButtonElement>(null);
  const { resource, save, index, select, move, hasPrevious, hasNext } = selection;
  const page = resource.data;
  useEffect(() => { selectedRef.current?.scrollIntoView({ block: "nearest", inline: "center" }); }, [save?.id]);

  return <section className={styles.saveDetails} aria-label={`${gameTitle} 的存档`}>
    <header className={styles.saveHeading}>
      <p>我的存档 {page && save ? <strong>{page.offset + index + 1} / {page.total}</strong> : null}</p>
      <span>左右切换 · A / Enter 从这里继续</span>
      <div className={styles.savePaging}>
        <button className="button secondary" aria-label="上一份存档" disabled={!hasPrevious} onClick={() => move("left")}>←</button>
        <button className="button secondary" aria-label="下一份存档" disabled={!hasNext} onClick={() => move("right")}>→</button>
      </div>
    </header>
    {resource.loading ? <p className={styles.saveState} role="status">正在读取存档…</p> : resource.error ? <div className={styles.saveState} role="alert">{resource.error}<button className="button secondary" onClick={resource.reload}>重试</button></div> : !save ? <p className={styles.saveState} role="status">当前游戏没有可用的存档。</p> : <>
      <div className={styles.saveRail}>
        {page?.items.map((item, position) => {
          const selected = position === index;
          const size = formatBytes(item.sizeBytes);
          return <button key={item.id} ref={selected ? selectedRef : undefined} type="button"
            className={`${styles.saveCard} ${selected ? styles.selectedSaveCard : ""}`}
            aria-label={`第 ${page.offset + position + 1} 份存档，${item.name}，${size}`}
            aria-pressed={selected} data-save-id={item.id} onClick={() => select(item.id)}>
            <span className={styles.saveScreenshot}>
              {item.screenshotUrl ? <Image src={item.screenshotUrl} alt={`${item.name}截图`} fill sizes={selected ? "30vw" : "15vw"} unoptimized /> : <span>暂无截图</span>}
              <SaveRestoreWarning save={item} thumbnail />
              <span className={styles.saveSize} aria-label={`存档大小 ${size}`} title={`${item.sizeBytes.toLocaleString("zh-CN")} bytes`}>{size}</span>
            </span>
            <BrowserTime value={item.updatedAtMs} />
          </button>;
        })}
      </div>
      <div className={styles.saveActions}>
        <div>
          <strong title={save.name}>{save.name}</strong>
          <p>{save.kind === "checkpoint" ? "即时存档" : "游戏内存档"} · {formatBytes(save.sizeBytes)}</p>
          <p><BrowserTime value={save.updatedAtMs} /></p>
          {save.restorable && save.kind === "game_save" ? <p>进入游戏后，请在游戏内读取存档。</p> : null}
        </div>
        <LaunchButton gameId={save.game.id} coreId={save.extinfo.coreId} saveId={save.id} disabled={!save.restorable} returnTo={returnTo}>从存档继续</LaunchButton>
      </div>
    </>}
  </section>;
}
