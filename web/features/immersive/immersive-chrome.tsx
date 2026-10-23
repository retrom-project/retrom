"use client";
import { useEffect, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import type { View } from "./immersive-data";
import { useImmersiveAudioContext } from "./immersive-audio-provider";
import styles from "./immersive.module.css";
function HelpButton({ button }: { button: string }) {
  if (button === "Select") { return <kbd data-button={button}><AppIcon name="more" /></kbd>; }
  if (button !== "horizontal" && button !== "vertical") { return <kbd data-button={button}>{button}</kbd>; }
  return <kbd data-button={button}><svg viewBox="0 0 24 24" aria-hidden="true"><path d={button === "horizontal" ? "M8 12h8M10 8l-4 4 4 4M14 8l4 4-4 4" : "M12 8v8M8 10l4-4 4 4M8 14l4 4 4-4"} /></svg></kbd>;
}
type Fullscreen = { active: boolean; supported: boolean; restoreVisible: boolean; enterFullscreen: () => Promise<boolean> };

/** Each view lists the controller hints that apply to it, in the order they are shown. */
function helpItems(view: View): readonly { buttons: readonly string[]; label: string }[] {
  if (view === "platforms") {
    return [
      { buttons: ["horizontal"], label: "选择平台" },
      { buttons: ["LB", "RB"], label: "跳 5 个" },
      { buttons: ["A"], label: "进入" },
      { buttons: ["B"], label: "退出沉浸模式" },
    ];
  }
  return [
    { buttons: ["vertical"], label: "选择游戏" },
    { buttons: ["LB", "RB"], label: "翻页" },
    // Recent games and saves are ordered by time, so only title-ordered lists jump by letter.
    ...(view === "games" || view === "favorites" ? [{ buttons: ["LT", "RT"], label: "按字母" }] : []),
    ...(view === "saves" ? [{ buttons: ["horizontal"], label: "切换存档" }] : []),
    { buttons: ["A"], label: view === "saves" ? "从存档继续" : "开始游戏" },
    { buttons: ["B"], label: "返回平台" },
    { buttons: ["Y"], label: "收藏" },
  ];
}

export function ImmersiveChrome({ view, fullscreen, onMenu }: { view: View; fullscreen: Fullscreen; onMenu: () => void }) {
  return <>
    <header className={styles.shellHeader}>
      <div><strong>RETROM</strong><span>/</span><span>沉浸模式</span></div>
      <HeaderControls fullscreen={fullscreen} />
    </header>
    <footer className={styles.helpBar} aria-label="手柄操作提示">
      {helpItems(view).map((item) => <span key={item.label} className={item.buttons.length > 1 ? styles.helpPair : undefined}>
        {item.buttons.map((button) => <HelpButton key={button} button={button} />)}{item.label}
      </span>)}
      <button className={styles.menuTrigger} onClick={onMenu}><HelpButton button="Select" />系统菜单</button>
    </footer>
  </>;
}

/** Background music and fullscreen recovery sit beside the clock, never over the content. */
function HeaderControls({ fullscreen }: { fullscreen: Fullscreen }) {
  const [clock, setClock] = useState<Date | null>(null);
  const audio = useImmersiveAudioContext();
  useEffect(() => {
    const initial = window.setTimeout(() => setClock(new Date()), 0);
    const timer = window.setInterval(() => setClock(new Date()), 30_000);
    return () => { window.clearTimeout(initial); window.clearInterval(timer); };
  }, []);
  return <div>
    {audio?.bgmState === "blocked" ? <button className={`${styles.fullscreenRestore} ${styles.fullscreenRestoreVisible}`} onClick={audio.retryBgm} title="浏览器暂停了背景音乐，点击或按任意键播放"><AppIcon name="music" />启用背景音乐</button> : null}
    {fullscreen.supported && !fullscreen.active ? <button className={`${styles.fullscreenRestore} ${fullscreen.restoreVisible ? styles.fullscreenRestoreVisible : ""}`} aria-hidden={!fullscreen.restoreVisible} tabIndex={fullscreen.restoreVisible ? 0 : -1} onClick={() => void fullscreen.enterFullscreen()}><AppIcon name="expand" />进入全屏</button> : null}
    <time dateTime={clock?.toISOString()}>{clock ? new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false }).format(clock) : "--:--"}</time>
  </div>;
}
