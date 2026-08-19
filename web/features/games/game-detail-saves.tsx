"use client";

import Image from "next/image";
import { useEffect, useRef, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { LaunchButton } from "@/features/player/launch-button";
import { formatSaveDuration, formatSaveTime, saveAvailable, type SaveItem } from "@/features/saves/save-library";

function SaveResume({ gameId, save, label, requiresThreads }: { gameId: string; save: SaveItem; label: string; requiresThreads: boolean }) {
  return saveAvailable(save)
    ? <LaunchButton gameId={gameId} saveStateId={save.saveStateId} returnTo={`/games/${gameId}`} requiresThreads={requiresThreads} label={label} />
    : <button className="button" type="button" disabled>当前不可继续</button>;
}

export function GameDetailSaves({ gameId, gameTitle, saves, nowMs, threadCoreIds = [] }: {
  gameId: string;
  gameTitle: string;
  saves: SaveItem[];
  nowMs: number;
  threadCoreIds?: string[];
}) {
  const recentSaves = saves.slice(0, 3);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [previewSave, setPreviewSave] = useState<SaveItem | null>(null);
  const drawerRef = useRef<HTMLElement>(null);
  const drawerTriggerRef = useRef<HTMLButtonElement>(null);
  const previewCloseRef = useRef<HTMLButtonElement>(null);
  const previewTriggerRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!drawerOpen && !previewSave) return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== "Escape") return;
      if (previewSave) setPreviewSave(null);
      else {
        setDrawerOpen(false);
        window.setTimeout(() => drawerTriggerRef.current?.focus(), 0);
      }
    }
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [drawerOpen, previewSave]);

  useEffect(() => {
    if (previewSave) previewCloseRef.current?.focus();
    else previewTriggerRef.current?.focus();
  }, [previewSave]);

  useEffect(() => {
    if (drawerOpen) drawerRef.current?.querySelector<HTMLElement>("button, a[href]")?.focus();
  }, [drawerOpen]);

  function closeDrawer() {
    setDrawerOpen(false);
    window.setTimeout(() => drawerTriggerRef.current?.focus(), 0);
  }

  function openPreview(save: SaveItem) {
    previewTriggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setPreviewSave(save);
  }

  return <>
    <section className="game-detail-saves" aria-labelledby="game-detail-saves-title">
      <header className="game-detail-saves-head">
        <div>
          <h2 id="game-detail-saves-title">游戏存档</h2>
          <p>最近 3 份可直接恢复；截图保持完整比例。</p>
        </div>
        <div className="game-detail-saves-actions">
          <span>共 {saves.length} 份</span>
          {saves.length ? <button ref={drawerTriggerRef} type="button" onClick={() => setDrawerOpen(true)}>查看全部存档</button> : null}
        </div>
      </header>
      {recentSaves.length ? <div className="game-detail-save-grid">
        {recentSaves.map((save, index) => <article className="game-detail-save-card" key={save.saveStateId}>
          <button className="game-detail-save-media" type="button" aria-label={`预览 ${formatSaveTime(save.createdAtMs, nowMs)} 的存档截图`} onClick={() => openPreview(save)}>
            {!saveAvailable(save) ? <span className="game-detail-save-blocked">当前不可用</span> : null}
            <Image src={save.screenshotUrl} alt="" fill sizes="(min-width: 1800px) 32vw, (min-width: 1600px) 290px, 220px" unoptimized />
          </button>
          <div className="game-detail-save-body">
            <div className="game-detail-save-title-line">
              <div><strong><time dateTime={new Date(save.createdAtMs).toISOString()}>{formatSaveTime(save.createdAtMs, nowMs)}</time></strong><small>{save.name || "手动存档"}</small></div>
              <span className={index === 0 ? "is-latest" : undefined}>{index === 0 ? "最近存档" : "手动"}</span>
            </div>
            <div className="game-detail-save-fact-row">
              <span><small>保存位置</small><b>{save.discLabel ?? (save.discIndex ? `光盘 ${save.discIndex}` : "主内容")}</b></span>
              <span><small>运行核心</small><b>{save.core.name}</b></span>
              <span><small>当时已游玩</small><b>{formatSaveDuration(save.activeDurationMs)}</b></span>
            </div>
            <SaveResume gameId={gameId} save={save} requiresThreads={threadCoreIds.includes(save.core.id)} label={index === 0 ? "▶ 从这里继续" : "恢复此存档"} />
          </div>
        </article>)}
      </div> : <div className="game-detail-saves-empty"><strong>还没有手动存档</strong><span>游玩时创建存档后，可以从这里快速恢复。</span></div>}
    </section>

    <div className={`game-detail-drawer-backdrop${drawerOpen ? " is-open" : ""}`} aria-hidden="true" onMouseDown={(event) => { if (event.target === event.currentTarget) closeDrawer(); }} />
    <aside
      ref={drawerRef}
      className={`game-detail-save-drawer${drawerOpen ? " is-open" : ""}`}
      role="dialog"
      aria-modal="true"
      aria-labelledby="game-detail-save-drawer-title"
      aria-hidden={!drawerOpen || Boolean(previewSave)}
      inert={!drawerOpen || Boolean(previewSave)}
      onKeyDown={(event) => {
        if (event.key !== "Tab") return;
        const focusable = Array.from(drawerRef.current?.querySelectorAll<HTMLElement>("button:not(:disabled), a[href], [tabindex]:not([tabindex='-1'])") ?? []);
        if (!focusable.length) return;
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
        {saves.map((save, index) => <article className="game-detail-drawer-row" key={save.saveStateId}>
          <button className="game-detail-drawer-shot" type="button" aria-label={`预览 ${formatSaveTime(save.createdAtMs, nowMs)} 的存档截图`} onClick={() => openPreview(save)}>
            <Image src={save.screenshotUrl} alt="" fill sizes="192px" unoptimized />
          </button>
          <div><time dateTime={new Date(save.createdAtMs).toISOString()}>{formatSaveTime(save.createdAtMs, nowMs)}</time><small>{save.core.name}{save.discLabel ? ` · ${save.discLabel}` : ""}{index === 0 ? " · 最近" : ""}</small></div>
          <SaveResume gameId={gameId} save={save} requiresThreads={threadCoreIds.includes(save.core.id)} label="▶ 继续" />
        </article>)}
      </div>
    </aside>

    {previewSave ? <div className="game-detail-preview" onMouseDown={(event) => { if (event.target === event.currentTarget) setPreviewSave(null); }}>
      <section role="dialog" aria-modal="true" aria-label="存档截图预览" onKeyDown={(event) => {
        if (event.key === "Tab") { event.preventDefault(); previewCloseRef.current?.focus(); }
      }}>
        <div className="game-detail-preview-image"><Image src={previewSave.screenshotUrl} alt={`${gameTitle} 存档截图完整预览`} width={1920} height={1080} unoptimized /></div>
        <footer><span>完整截图 · 保持原始画面比例</span><button ref={previewCloseRef} type="button" onClick={() => setPreviewSave(null)}>关闭</button></footer>
      </section>
    </div> : null}
  </>;
}
