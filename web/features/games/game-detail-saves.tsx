"use client";

import {saveDisplayTime} from "@/features/saves/save-library";

import { useEffect, useRef, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { LaunchButton } from "@/features/player/launch-button";
import { formatSaveDuration, saveAvailable, type SaveItem } from "@/features/saves/save-library";
import { SaveScreenshot } from "@/features/saves/save-screenshot";
import { SaveSizeLabel } from "@/features/saves/save-size-label";
import { useSaveTimeFormatter } from "@/features/saves/use-save-time";

function SaveResume({ gameId, save, label, requiresThreads, secondary = false }: { gameId: string; save: SaveItem; label: string; requiresThreads: boolean; secondary?: boolean }) {
  return saveAvailable(save)
    ? <LaunchButton secondary={secondary} gameId={gameId} saveStateId={save.saveStateId} returnTo={`/games/${gameId}`} requiresThreads={requiresThreads} label={label} />
    : <button className={secondary ? "button secondary" : "button"} type="button" disabled>当前不可继续</button>;
}

export function GameDetailSaves({ gameId, gameTitle, saves, nowMs, threadCoreIds = [] }: {
  gameId: string;
  gameTitle: string;
  saves: SaveItem[];
  nowMs: number;
  threadCoreIds?: string[];
}) {
  const formatTime = useSaveTimeFormatter();
  const recentSaves = saves.slice(0, 3);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const drawerRef = useRef<HTMLElement>(null);
  const drawerTriggerRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!drawerOpen) {return;}
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== "Escape") {return;}
      setDrawerOpen(false);
      window.setTimeout(() => drawerTriggerRef.current?.focus(), 0);
    }
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [drawerOpen]);

  useEffect(() => {
    if (drawerOpen) {drawerRef.current?.querySelector<HTMLElement>("button, a[href]")?.focus();}
  }, [drawerOpen]);

  function closeDrawer() {
    setDrawerOpen(false);
    window.setTimeout(() => drawerTriggerRef.current?.focus(), 0);
  }

  return <>
    <section className="game-detail-saves" aria-labelledby="game-detail-saves-title">
      <header className="game-detail-saves-head">
        <div>
          <h2 id="game-detail-saves-title">游戏存档</h2>
          <p>最近 3 份游戏数据；原生存档恢复后请在游戏内读档。</p>
        </div>
        <div className="game-detail-saves-actions">
          <span>共 {saves.length} 份</span>
          {saves.length ? <button ref={drawerTriggerRef} type="button" onClick={() => setDrawerOpen(true)}>查看全部存档</button> : null}
        </div>
      </header>
      {recentSaves.length ? <div className="game-detail-save-grid">
        {recentSaves.map((save) => <article className="game-detail-save-card" key={save.saveStateId}>
          <div className="game-detail-save-media">
            {!saveAvailable(save) ? <span className="game-detail-save-blocked">当前不可用</span> : null}
            <SaveScreenshot screenshotUrl={save.screenshotUrl} alt="存档截图" sizes="(min-width: 1600px) 220px, (min-width: 768px) 30vw, 120px" />
            <SaveSizeLabel sizeBytes={save.sizeBytes} />
          </div>
          <div className="game-detail-save-body">
            <div className="game-detail-save-title-line">
              <div><strong><time dateTime={new Date(saveDisplayTime(save)).toISOString()}>{formatTime(saveDisplayTime(save), nowMs)}</time></strong><small>{save.name || "手动存档"}</small></div>
            </div>
            <div className="game-detail-save-fact-row">
              <span><small>保存位置</small><b>{save.discLabel ?? (save.discIndex ? `光盘 ${save.discIndex}` : "主内容")}</b></span>
              <span><small>运行核心</small><b>{save.core.name}</b></span>
              <span><small>当时已游玩</small><b>{formatSaveDuration(save.activeDurationMs)}</b></span>
            </div>
            <SaveResume secondary gameId={gameId} save={save} requiresThreads={threadCoreIds.includes(save.core.id)} label="从存档继续" />
          </div>
        </article>)}
      </div> : <div className="game-detail-saves-empty"><strong>还没有存档</strong><span>游玩时创建存档后，可以从这里快速恢复。</span></div>}
    </section>

    <div className={`game-detail-drawer-backdrop${drawerOpen ? " is-open" : ""}`} aria-hidden="true" onMouseDown={(event) => { if (event.target === event.currentTarget) {closeDrawer();} }} />
    <aside
      ref={drawerRef}
      className={`game-detail-save-drawer${drawerOpen ? " is-open" : ""}`}
      role="dialog"
      aria-modal="true"
      aria-labelledby="game-detail-save-drawer-title"
      aria-hidden={!drawerOpen}
      inert={!drawerOpen}
      onKeyDown={(event) => {
        if (event.key !== "Tab") {return;}
        const focusable = Array.from(drawerRef.current?.querySelectorAll<HTMLElement>("button:not(:disabled), a[href], [tabindex]:not([tabindex='-1'])") ?? []);
        if (!focusable.length) {return;}
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
      }}
    >
      <header>
        <div><h2 id="game-detail-save-drawer-title">全部存档</h2><p>{gameTitle} · 共 {saves.length} 份</p></div>
        <button className="game-detail-drawer-close" type="button" aria-label="关闭全部存档" onClick={closeDrawer}><AppIcon name="x" /></button>
      </header>
      <div className="game-detail-drawer-body">
        {saves.map((save) => <article className="game-detail-drawer-row" key={save.saveStateId}>
          <div className="game-detail-drawer-shot">
            <SaveScreenshot screenshotUrl={save.screenshotUrl} alt="存档截图" sizes="192px" />
            <SaveSizeLabel sizeBytes={save.sizeBytes} />
          </div>
          <div><time dateTime={new Date(saveDisplayTime(save)).toISOString()}>{formatTime(saveDisplayTime(save), nowMs)}</time><small>{save.core.name}{save.discLabel ? ` · ${save.discLabel}` : ""}</small></div>
          <SaveResume gameId={gameId} save={save} requiresThreads={threadCoreIds.includes(save.core.id)} label="从存档继续" />
        </article>)}
      </div>
    </aside>

  </>;
}
