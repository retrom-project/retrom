"use client";

import { useEffect, useRef, useState } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import type { EmulatorSettingsPanel } from "./emulator-settings";

type SyncTone = "synced" | "busy" | "warning";

export function PlayerChrome({
  controlsVisible,
  running,
  paused,
  fullscreen,
  gameTitle,
  coreName,
  platformName,
  syncText,
  syncTone,
  toast,
  warnings,
  hasPersistentConflict,
  emulatorToolbarOpen,
  emulatorVolume,
  emulatorMuted,
  onHoldControls,
  onReleaseControls,
  onSave,
  onPauseForToolbarInteraction,
  onToggleFullscreen,
  onOpenEmulatorSettings,
  onCloseEmulatorSettings,
  onOpenEmulatorPanel,
  onChangeEmulatorVolume,
  onToggleEmulatorMute,
  onExit,
  onDownloadConflict,
}: {
  controlsVisible: boolean;
  running: boolean;
  paused: boolean;
  fullscreen: boolean;
  gameTitle: string;
  coreName: string;
  platformName: string;
  syncText: string;
  syncTone: SyncTone;
  toast: string;
  warnings: string[];
  hasPersistentConflict: boolean;
  emulatorToolbarOpen: boolean;
  emulatorVolume: number;
  emulatorMuted: boolean;
  onHoldControls: () => void;
  onReleaseControls: () => void;
  onSave: () => void;
  onPauseForToolbarInteraction: () => void;
  onToggleFullscreen: () => void;
  onOpenEmulatorSettings: () => void;
  onCloseEmulatorSettings: () => void;
  onOpenEmulatorPanel: (panel: EmulatorSettingsPanel) => void;
  onChangeEmulatorVolume: (volume: number) => void;
  onToggleEmulatorMute: () => void;
  onExit: () => void;
  onDownloadConflict: () => void;
}) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [exitOpen, setExitOpen] = useState(false);
  const [conflictDismissed, setConflictDismissed] = useState(false);
  const [localToast, setLocalToast] = useState("");
  const menuRef = useRef<HTMLDivElement>(null);
  const toolbarRef = useRef<HTMLElement>(null);
  const toolbarHovered = useRef(false);
  const toolbarFocused = useRef(false);

  useEffect(() => {
    if (!menuOpen) return;
    const closeOnOutside = (event: PointerEvent) => {
      if (event.target instanceof Node && !menuRef.current?.contains(event.target)) setMenuOpen(false);
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setMenuOpen(false);
    };
    document.addEventListener("pointerdown", closeOnOutside);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("pointerdown", closeOnOutside);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [menuOpen]);

  useEffect(() => {
    if (menuOpen || exitOpen || emulatorToolbarOpen) onHoldControls();
    else if (!toolbarHovered.current && !toolbarFocused.current) onReleaseControls();
  }, [emulatorToolbarOpen, exitOpen, menuOpen, onHoldControls, onReleaseControls]);

  useEffect(() => {
    if (!localToast) return;
    const timer = window.setTimeout(() => setLocalToast(""), 2_400);
    return () => window.clearTimeout(timer);
  }, [localToast]);

  const visibleToast = localToast || toast;
  const warningCopy = warnings.includes("BIOS_HASH_WARNING")
    ? "BIOS 校验值与目录期望不同，但当前允许运行。"
    : "当前运行环境有需要留意的提示。";

  function requestExit() {
    setMenuOpen(false);
    setExitOpen(true);
  }

  return <>
    <header
      ref={toolbarRef}
      className={`player-toolbar${controlsVisible || paused ? " is-visible" : ""}`}
      onClickCapture={onPauseForToolbarInteraction}
      onBlurCapture={(event) => {
        if (event.relatedTarget instanceof Node && toolbarRef.current?.contains(event.relatedTarget)) return;
        toolbarFocused.current = false;
        if (!menuOpen && !exitOpen && !emulatorToolbarOpen) onReleaseControls();
      }}
      onFocusCapture={() => { toolbarFocused.current = true; onHoldControls(); }}
      onPointerEnter={() => { toolbarHovered.current = true; onHoldControls(); }}
      onPointerLeave={() => { toolbarHovered.current = false; if (!menuOpen && !exitOpen && !emulatorToolbarOpen) onReleaseControls(); }}
      onPointerMove={(event) => event.stopPropagation()}
    >
      <button className="player-back" type="button" aria-label="返回并退出游戏" title="返回并退出游戏" onClick={requestExit}>
        <AppIcon name="arrow-left" />
      </button>
      <div className="player-game-meta">
        <strong>{gameTitle}</strong>
        <span>{[coreName, platformName].filter(Boolean).join(" · ")}</span>
      </div>
      <div className={`player-sync-status is-${syncTone}`} role="status" aria-live="polite">
        <i aria-hidden="true" />
        <span>{syncText}</span>
        {warnings.length ? <button className="player-warning-dot" type="button" aria-label="查看运行提醒" title="查看运行提醒" onClick={() => setLocalToast(warningCopy)} /> : null}
      </div>
      <div className="player-actions">
        <button className="player-control player-save-button" type="button" disabled={!running} onClick={onSave}><AppIcon name="save" />创建存档</button>
        <button className="player-control is-icon" type="button" aria-label={paused ? "已暂停，点击游戏画面继续" : "暂停"} title={paused ? "点击游戏画面继续" : "暂停"} aria-pressed={paused} disabled={!running}><AppIcon name="pause" /></button>
        <button className="player-control is-icon" type="button" aria-label={fullscreen ? "退出全屏" : "全屏"} title={fullscreen ? "退出全屏" : "全屏"} onClick={onToggleFullscreen}><AppIcon name={fullscreen ? "minimize" : "maximize"} /></button>
        <div className="player-menu-wrap" ref={menuRef}>
          <button className="player-control is-icon" type="button" aria-label="更多操作" title="更多操作" aria-expanded={menuOpen} aria-haspopup="menu" onClick={() => setMenuOpen((open) => !open)}><AppIcon name="more" /></button>
          {menuOpen ? <div className="player-menu" role="menu">
            <button type="button" role="menuitem" onClick={() => { setMenuOpen(false); onOpenEmulatorSettings(); }}><AppIcon name="settings" />模拟器设置</button>
            <button type="button" role="menuitem" onClick={() => { setMenuOpen(false); setLocalToast("将鼠标移到屏幕顶部可显示控制栏；Esc 只退出浏览器全屏，不会退出游戏。"); }}><AppIcon name="keyboard" />查看快捷键</button>
            {warnings.length ? <button type="button" role="menuitem" onClick={() => { setMenuOpen(false); setLocalToast(warningCopy); }}><AppIcon name="warning" />查看运行提醒</button> : null}
            <hr />
            <button className="is-danger" type="button" role="menuitem" onClick={requestExit}><AppIcon name="log-out" />退出游戏</button>
          </div> : null}
        </div>
      </div>
    </header>

    <div className={`player-pause-overlay${paused ? " is-visible" : ""}`} aria-hidden={!paused}>
      <div className="player-pause-pill"><AppIcon name="pause" /><strong>已暂停</strong><small>点击游戏画面继续</small></div>
    </div>

    <section
      className={`player-emulator-toolbar${emulatorToolbarOpen ? " is-open" : ""}`}
      aria-label="模拟器设置工具栏"
      aria-hidden={!emulatorToolbarOpen}
      onFocusCapture={onHoldControls}
      onPointerEnter={onHoldControls}
    >
      <div className="player-emulator-group">
        <span className="player-emulator-label">模拟器</span>
        <button type="button" disabled={!emulatorToolbarOpen} onClick={() => onOpenEmulatorPanel("controls")}><span aria-hidden="true">🎮</span>控制</button>
        <button type="button" disabled={!emulatorToolbarOpen} onClick={() => onOpenEmulatorPanel("display")}><span aria-hidden="true">▤</span>显示</button>
        <button type="button" disabled={!emulatorToolbarOpen} onClick={() => onOpenEmulatorPanel("core")}><span aria-hidden="true">⚙</span>Core 设置</button>
      </div>
      <div className="player-emulator-group">
        <label className="player-emulator-volume">
          <span className="player-emulator-label">音量</span>
          <input
            type="range"
            min="0"
            max="100"
            step="1"
            value={Math.round(emulatorVolume * 100)}
            aria-label="模拟器音量"
            aria-valuetext={emulatorMuted ? `已静音，音量 ${Math.round(emulatorVolume * 100)}%` : `${Math.round(emulatorVolume * 100)}%`}
            disabled={!emulatorToolbarOpen}
            onChange={(event) => onChangeEmulatorVolume(Number(event.currentTarget.value) / 100)}
          />
        </label>
        <button type="button" disabled={!emulatorToolbarOpen} aria-label={emulatorMuted ? "取消静音" : "静音"} aria-pressed={emulatorMuted} onClick={onToggleEmulatorMute}><span aria-hidden="true">{emulatorMuted ? "🔇" : "🔊"}</span></button>
        <button type="button" disabled={!emulatorToolbarOpen} onClick={onCloseEmulatorSettings}>收起</button>
      </div>
    </section>

    {!conflictDismissed && hasPersistentConflict ? <aside className="player-conflict" role="alert">
      <span className="player-conflict-mark" aria-hidden="true">!</span>
      <div><strong>另一游戏会话更新了服务器存档</strong><p>当前本地进度尚未同步。退出前建议先保存一份本地副本。</p></div>
      <div className="player-conflict-actions">
        <button className="player-control" type="button" onClick={onDownloadConflict}><AppIcon name="download" />下载本地存档</button>
        <button className="player-control" type="button" onClick={() => setConflictDismissed(true)}>稍后处理</button>
      </div>
    </aside> : null}

    <div className={`player-toast${visibleToast ? " is-visible" : ""}`} role="status" aria-live="polite">{visibleToast}</div>
    <div className={`player-controls-hint${controlsVisible ? " is-hidden" : ""}`}>移到屏幕顶部显示 Retrom 控制</div>

    <ConfirmDialog
      open={exitOpen}
      title="退出游戏？"
      description={hasPersistentConflict ? "检测到尚未同步的本地进度。" : "游戏进度已同步，可以安全退出。"}
      confirmLabel="退出游戏"
      secondaryLabel={hasPersistentConflict ? "下载本地存档并退出" : undefined}
      tone="danger"
      onCancel={() => setExitOpen(false)}
      onSecondary={hasPersistentConflict ? () => { onDownloadConflict(); setExitOpen(false); onExit(); } : undefined}
      onConfirm={() => { setExitOpen(false); onExit(); }}
    >
      {hasPersistentConflict ? <span>服务器存档已由另一会话推进；直接退出不会覆盖服务器版本，重新进入游戏后将从服务器最新版本开始。</span> : <span>退出时会刷新持久进度、结束本次游玩记录，并返回启动游戏前的页面。</span>}
    </ConfirmDialog>
  </>;
}
