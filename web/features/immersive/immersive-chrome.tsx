"use client";
import { useEffect, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import type { View } from "./immersive-data";
import styles from "./immersive.module.css";
function HelpButton({ button }: { button: string }) {
  if (button === "Select") { return <kbd data-button={button}><AppIcon name="more" /></kbd>; }
  if (button !== "horizontal" && button !== "vertical") { return <kbd data-button={button}>{button}</kbd>; }
  return <kbd data-button={button}><svg viewBox="0 0 24 24" aria-hidden="true"><path d={button === "horizontal" ? "M8 12h8M10 8l-4 4 4 4M14 8l4 4-4 4" : "M12 8v8M8 10l4-4 4 4M8 14l4 4 4-4"} /></svg></kbd>;
}
export function ImmersiveChrome({ view, fullscreen, onMenu }: {
  view: View;
  fullscreen: { active: boolean; supported: boolean; restoreVisible: boolean; enterFullscreen: () => Promise<boolean> };
  onMenu: () => void;
}) {
  const [clock, setClock] = useState<Date | null>(null);
  useEffect(() => {
    const initial = window.setTimeout(() => setClock(new Date()), 0);
    const timer = window.setInterval(() => setClock(new Date()), 30_000);
    return () => { window.clearTimeout(initial); window.clearInterval(timer); };
  }, []);
  return <>
    <header className={styles.shellHeader}>
      <div><strong>RETROM</strong><span>/</span><span>沉浸模式</span></div>
      <div>
        {fullscreen.supported && !fullscreen.active ? <button className={`${styles.fullscreenRestore} ${fullscreen.restoreVisible ? styles.fullscreenRestoreVisible : ""}`} aria-hidden={!fullscreen.restoreVisible} tabIndex={fullscreen.restoreVisible ? 0 : -1} onClick={() => void fullscreen.enterFullscreen()}><AppIcon name="expand" />进入全屏</button> : null}
        <time dateTime={clock?.toISOString()}>{clock ? new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit", hour12: false }).format(clock) : "--:--"}</time>
      </div>
    </header>
    <footer className={styles.helpBar} aria-label="手柄操作提示">
      <span><HelpButton button={view === "platforms" ? "horizontal" : "vertical"} />{view === "platforms" ? "选择平台" : view === "saves" ? "选择存档" : "选择游戏"}</span>
      <span><HelpButton button="A" />{view === "platforms" ? "进入" : view === "saves" ? "从存档继续" : "开始游戏"}</span>
      <span><HelpButton button="B" />{view === "platforms" ? "退出沉浸模式" : "返回平台"}</span>
      {view !== "platforms" ? <span><HelpButton button="Y" />收藏</span> : null}
      <button className={styles.menuTrigger} onClick={onMenu}><HelpButton button="Select" />系统菜单</button>
    </footer>
  </>;
}
