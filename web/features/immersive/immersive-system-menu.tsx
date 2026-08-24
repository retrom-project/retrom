"use client";

import { useEffect, useRef } from "react";
import type { ImmersiveAudioPreferences } from "./immersive-audio-preferences";
import {
  immersiveSystemMenuItems,
  type ImmersiveSystemMenuItem,
  type MenuAdjustment,
} from "./system-menu-model";
import styles from "./system-menu.module.css";

const labels: Record<ImmersiveSystemMenuItem, string> = {
  "bgm-volume": "背景音乐音量",
  "bgm-muted": "背景音乐静音",
  "game-volume": "游戏音量",
  "game-muted": "游戏静音",
  fullscreen: "进入全屏",
  exit: "退出沉浸模式",
};

function VolumeControl({ item, value, onAdjust }: {
  item: Extract<ImmersiveSystemMenuItem, "bgm-volume" | "game-volume">;
  value: number;
  onAdjust: (item: ImmersiveSystemMenuItem, adjustment: MenuAdjustment) => void;
}) {
  const percentage = Math.round(value * 100);
  return <div className={styles.volumeControl}>
    <button type="button" aria-label={`${labels[item]}降低`} onClick={() => onAdjust(item, "left")}>−</button>
    <div role="meter" aria-label={labels[item]} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percentage}>
      <i style={{ width: `${percentage}%` }} />
    </div>
    <strong>{percentage}%</strong>
    <button type="button" aria-label={`${labels[item]}提高`} onClick={() => onAdjust(item, "right")}>＋</button>
  </div>;
}

function MenuValue({ item, preferences, fullscreenActive, fullscreenSupported, onAdjust }: {
  item: ImmersiveSystemMenuItem;
  preferences: ImmersiveAudioPreferences;
  fullscreenActive: boolean;
  fullscreenSupported: boolean;
  onAdjust: (item: ImmersiveSystemMenuItem, adjustment: MenuAdjustment) => void;
}) {
  if (item === "bgm-volume") {return <VolumeControl item={item} value={preferences.bgmVolume} onAdjust={onAdjust} />;}
  if (item === "game-volume") {return <VolumeControl item={item} value={preferences.gameVolume} onAdjust={onAdjust} />;}
  if (item === "bgm-muted" || item === "game-muted") {
    const muted = item === "bgm-muted" ? preferences.bgmMuted : preferences.gameMuted;
    return <span className={styles.toggle} data-active={muted}>{muted ? "已静音" : "有声音"}</span>;
  }
  if (item === "fullscreen") {
    return <span>{fullscreenActive ? "已全屏" : fullscreenSupported ? "A 确认" : "不支持"}</span>;
  }
  return <span className={styles.exitHint}>返回普通首页</span>;
}

export function ImmersiveSystemMenu({ announcement, fullscreenActive, fullscreenSupported, preferences, selectedIndex, onActivate, onAdjust, onClose, onSelect }: {
  announcement: string;
  fullscreenActive: boolean;
  fullscreenSupported: boolean;
  preferences: ImmersiveAudioPreferences;
  selectedIndex: number;
  onActivate: (item: ImmersiveSystemMenuItem) => void;
  onAdjust: (item: ImmersiveSystemMenuItem, adjustment: MenuAdjustment) => void;
  onClose: () => void;
  onSelect: (index: number) => void;
}) {
  const selectedRef = useRef<HTMLButtonElement>(null);
  useEffect(() => selectedRef.current?.focus(), [selectedIndex]);
  return <div className={styles.backdrop}>
    <section className={styles.menu} role="dialog" aria-modal="true" aria-labelledby="immersive-system-menu-title" aria-describedby="immersive-system-menu-description">
      <header><p>RETROM</p><h2 id="immersive-system-menu-title">系统菜单</h2><span id="immersive-system-menu-description">调整沉浸模式的声音与显示</span></header>
      <div className={styles.options} role="group" aria-label="系统菜单选项">
        {immersiveSystemMenuItems.map((item, index) => <div
          key={item}
          className={styles.option}
          data-selected={index === selectedIndex}
          aria-current={index === selectedIndex ? "true" : undefined}
        >
          <button
            ref={index === selectedIndex ? selectedRef : undefined}
            type="button"
            onFocus={() => onSelect(index)}
            onClick={() => onActivate(item)}
          >{labels[item]}</button>
          <MenuValue {...{ item, preferences, fullscreenActive, fullscreenSupported, onAdjust }} />
        </div>)}
      </div>
      <p className={styles.announcement} role="status" aria-live="polite">{announcement}</p>
      <footer><span>Select 菜单</span><span>上下选择</span><span>左右调整</span><span>A 确认</span><button type="button" onClick={onClose}>B 返回</button></footer>
    </section>
  </div>;
}

export function ImmersiveBgmPrompt({ onRetry }: { onRetry: () => void }) {
  return <div className={styles.audioPrompt} role="status" aria-live="polite">
    <span>背景音乐等待播放</span>
    <button type="button" onClick={onRetry}>启用背景音乐</button>
  </div>;
}
