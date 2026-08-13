"use client";

import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import type { EmulatorSettingsPanel } from "./emulator-settings";
import type { DiscSet, DiscState } from "./adapters/ejs-4.2.3-v2";
import type { PlayerDebugMetrics } from "./player-debug";

type SyncTone = "synced" | "busy" | "warning";
type ExitSaveState = "idle" | "saving" | "saved" | "error";

export type PlayerDebugRuntime = {
  coreId: string;
  coreArtifactId: string;
  emulatorJSVersion: string;
  playerAdapterId: string;
  inputMode: string;
  crossOriginIsolated: boolean;
  sharedArrayBuffer: boolean;
};

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
  discSet,
  discState,
  netplayPlayerNo,
  netplayPaused,
  debugOpen,
  debugMetrics,
  debugRuntime,
  runtimeState,
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
  onSelectDisc,
  onToggleNetplayPause,
  onToggleDebug,
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
  discSet: DiscSet | null;
  discState: DiscState | null;
  netplayPlayerNo: number | null;
  netplayPaused: boolean;
  debugOpen: boolean;
  debugMetrics: PlayerDebugMetrics | null;
  debugRuntime: PlayerDebugRuntime;
  runtimeState: "loading" | "running" | "error";
  onHoldControls: () => void;
  onReleaseControls: () => void;
  onSave: () => Promise<boolean>;
  onPauseForToolbarInteraction: () => void;
  onToggleFullscreen: () => void;
  onOpenEmulatorSettings: () => void;
  onCloseEmulatorSettings: () => void;
  onOpenEmulatorPanel: (panel: EmulatorSettingsPanel) => void;
  onChangeEmulatorVolume: (volume: number) => void;
  onToggleEmulatorMute: () => void;
  onSelectDisc: (index: number) => Promise<boolean>;
  onToggleNetplayPause: () => void;
  onToggleDebug: () => void;
  onExit: () => void;
  onDownloadConflict: () => void;
}) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [discMenuOpen, setDiscMenuOpen] = useState(false);
  const [discBusy, setDiscBusy] = useState(false);
  const [exitOpen, setExitOpen] = useState(false);
  const [exitSaveState, setExitSaveState] = useState<ExitSaveState>("idle");
  const [conflictDismissed, setConflictDismissed] = useState(false);
  const [localToast, setLocalToast] = useState("");
  const menuRef = useRef<HTMLDivElement>(null);
  const discMenuRef = useRef<HTMLDivElement>(null);
  const discButtonRef = useRef<HTMLButtonElement>(null);
  const toolbarRef = useRef<HTMLElement>(null);
  const toolbarHovered = useRef(false);
  const toolbarFocused = useRef(false);
  const exitSavePending = useRef(false);

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
    if (!discMenuOpen) return;
    const items = Array.from(discMenuRef.current?.querySelectorAll<HTMLButtonElement>("[role=\"menuitemradio\"]") ?? []);
    (items.find((item) => item.getAttribute("aria-checked") === "true") ?? items[0])?.focus();
    const close = (focusButton = false) => {
      setDiscMenuOpen(false);
      if (focusButton) discButtonRef.current?.focus();
    };
    const closeOnOutside = (event: PointerEvent) => {
      if (event.target instanceof Node && !discMenuRef.current?.contains(event.target)) close();
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") close(true);
    };
    document.addEventListener("pointerdown", closeOnOutside);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("pointerdown", closeOnOutside);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [discMenuOpen]);

  useEffect(() => {
    if (menuOpen || discMenuOpen || exitOpen || emulatorToolbarOpen || debugOpen) onHoldControls();
    else if (!toolbarHovered.current && !toolbarFocused.current) onReleaseControls();
  }, [debugOpen, discMenuOpen, emulatorToolbarOpen, exitOpen, menuOpen, onHoldControls, onReleaseControls]);

  useEffect(() => {
    if (!localToast) return;
    const timer = window.setTimeout(() => setLocalToast(""), 2_400);
    return () => window.clearTimeout(timer);
  }, [localToast]);

  const visibleToast = localToast || toast;
  const isNetplay = netplayPlayerNo !== null;
  const warningCopy = warnings.includes("BIOS_HASH_WARNING")
    ? "BIOS 校验值与目录期望不同，但当前允许运行。"
    : "当前运行环境有需要留意的提示。";

  function requestExit() {
    setMenuOpen(false);
    exitSavePending.current = false;
    setExitSaveState("idle");
    setExitOpen(true);
  }

  async function createExitSave() {
    if (isNetplay || !running || exitSavePending.current || exitSaveState === "saved") return;
    exitSavePending.current = true;
    onPauseForToolbarInteraction();
    setExitSaveState("saving");
    const saved = await onSave().catch(() => false);
    exitSavePending.current = false;
    setExitSaveState(saved ? "saved" : "error");
  }

  async function chooseDisc(index: number) {
    if (!discState || discBusy) return;
    if (index === discState.currentIndex) {
      setDiscMenuOpen(false);
      discButtonRef.current?.focus();
      return;
    }
    setDiscBusy(true);
    const changed = await onSelectDisc(index).catch(() => false);
    setDiscBusy(false);
    if (changed) {
      setDiscMenuOpen(false);
      discButtonRef.current?.focus();
    }
  }

  function moveDiscMenuFocus(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (!["ArrowDown", "ArrowRight", "ArrowUp", "ArrowLeft", "Home", "End"].includes(event.key)) return;
    const items = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>("[role=\"menuitemradio\"]:not(:disabled)"));
    if (!items.length) return;
    event.preventDefault();
    const current = items.indexOf(document.activeElement as HTMLButtonElement);
    const target = event.key === "Home"
      ? 0
      : event.key === "End"
        ? items.length - 1
        : event.key === "ArrowDown" || event.key === "ArrowRight"
          ? (current + 1 + items.length) % items.length
          : (current - 1 + items.length) % items.length;
    items[target]?.focus();
  }

  const exitDescription = isNetplay
    ? "退出会结束所有参与者的本局联机，并返回房间。联机模式不会读取或写入个人存档。"
    : exitSaveState === "saving"
    ? "正在创建退出前存档…"
    : exitSaveState === "saved"
      ? "退出前存档已创建，可以安全退出。"
      : exitSaveState === "error"
        ? "创建存档失败，未生成不完整记录。可以重试或取消后继续游戏。"
        : hasPersistentConflict
          ? "检测到尚未同步的本地进度。"
          : "游戏进度已同步，可以安全退出。";

  return <>
    <header
      ref={toolbarRef}
      className={`player-toolbar${controlsVisible || paused ? " is-visible" : ""}`}
      onClickCapture={(event) => {
        const target = event.target;
        if (target instanceof Element && target.closest(".player-disc-wrap,.player-debug-control")) return;
        onPauseForToolbarInteraction();
      }}
      onBlurCapture={(event) => {
        if (event.relatedTarget instanceof Node && toolbarRef.current?.contains(event.relatedTarget)) return;
        toolbarFocused.current = false;
        if (!menuOpen && !discMenuOpen && !exitOpen && !emulatorToolbarOpen && !debugOpen) onReleaseControls();
      }}
      onFocusCapture={() => { toolbarFocused.current = true; onHoldControls(); }}
      onPointerEnter={() => { toolbarHovered.current = true; onHoldControls(); }}
      onPointerLeave={() => { toolbarHovered.current = false; if (!menuOpen && !discMenuOpen && !exitOpen && !emulatorToolbarOpen && !debugOpen) onReleaseControls(); }}
      onPointerMove={(event) => event.stopPropagation()}
    >
      <button className="player-back" type="button" aria-label="返回并退出游戏" title="返回并退出游戏" onClick={requestExit}>
        <AppIcon name="arrow-left" />
      </button>
      <div className="player-game-meta">
        <strong>{gameTitle}</strong>
        <span>{[coreName, platformName, isNetplay ? `联机 · P${netplayPlayerNo}` : ""].filter(Boolean).join(" · ")}</span>
      </div>
      <div className={`player-sync-status is-${syncTone}`} role="status" aria-live="polite">
        <i aria-hidden="true" />
        <span>{syncText}</span>
        {warnings.length ? <button className="player-warning-dot" type="button" aria-label="查看运行提醒" title="查看运行提醒" onClick={() => setLocalToast(warningCopy)} /> : null}
      </div>
      <div className="player-actions">
        <button className="player-control player-debug-control" type="button" aria-expanded={debugOpen} aria-controls="player-debug-panel" aria-pressed={debugOpen} onClick={onToggleDebug}><AppIcon name="chip" />调试信息</button>
        {!isNetplay && discSet && discState ? <div className="player-menu-wrap player-disc-wrap" ref={discMenuRef}>
          <button
            ref={discButtonRef}
            className="player-control player-disc-button"
            type="button"
            disabled={!running || discBusy}
            aria-label={`光盘 ${discState.currentIndex + 1} / ${discSet.count}`}
            aria-expanded={discMenuOpen}
            aria-haspopup="menu"
            onClick={() => setDiscMenuOpen((open) => !open)}
          >
            <span aria-hidden="true">◉</span>光盘 {discState.currentIndex + 1} / {discSet.count}
          </button>
          {discMenuOpen ? <div className="player-menu player-disc-menu" role="menu" aria-label="选择光盘" onKeyDown={moveDiscMenuFocus}>
            <strong>选择光盘</strong>
            {discSet.entries.map((entry) => <button
              key={entry.index}
              type="button"
              role="menuitemradio"
              aria-checked={entry.index === discState.currentIndex}
              disabled={discBusy}
              onClick={() => void chooseDisc(entry.index)}
            >
              <span aria-hidden="true">{entry.index === discState.currentIndex ? "✓" : "○"}</span>
              {entry.label}{entry.index === discState.currentIndex ? " · 当前" : ""}
            </button>)}
            <small>切换后游戏保持暂停，返回游戏即可继续。</small>
          </div> : null}
        </div> : null}
        {!isNetplay ? <button className="player-control player-save-button" type="button" disabled={!running} onClick={() => void onSave()}><AppIcon name="save" />创建存档</button> : null}
        {isNetplay && netplayPlayerNo === 1 ? <button className="player-control" type="button" disabled={!running} aria-pressed={netplayPaused} onClick={onToggleNetplayPause}><AppIcon name={netplayPaused ? "play" : "pause"} />{netplayPaused ? "继续联机" : "全局暂停"}</button> : null}
        {!isNetplay ? <button className="player-control is-icon" type="button" aria-label={paused ? "已暂停，点击游戏画面继续" : "暂停"} title={paused ? "点击游戏画面继续" : "暂停"} aria-pressed={paused} disabled={!running}><AppIcon name="pause" /></button> : null}
        <button className="player-control is-icon" type="button" aria-label={fullscreen ? "退出全屏" : "全屏"} title={fullscreen ? "退出全屏" : "全屏"} onClick={onToggleFullscreen}><AppIcon name={fullscreen ? "minimize" : "maximize"} /></button>
        <div className="player-menu-wrap" ref={menuRef}>
          <button className="player-control is-icon" type="button" aria-label="更多操作" title="更多操作" aria-expanded={menuOpen} aria-haspopup="menu" onClick={() => setMenuOpen((open) => !open)}><AppIcon name="more" /></button>
          {menuOpen ? <div className="player-menu" role="menu">
            {!isNetplay ? <button type="button" role="menuitem" onClick={() => { setMenuOpen(false); onOpenEmulatorSettings(); }}><AppIcon name="settings" />模拟器设置</button> : null}
            <button type="button" role="menuitem" onClick={() => { setMenuOpen(false); setLocalToast("将鼠标移到屏幕顶部可显示控制栏；Esc 只退出浏览器全屏，不会退出游戏。"); }}><AppIcon name="keyboard" />查看快捷键</button>
            {warnings.length ? <button type="button" role="menuitem" onClick={() => { setMenuOpen(false); setLocalToast(warningCopy); }}><AppIcon name="warning" />查看运行提醒</button> : null}
            <hr />
            <button className="is-danger" type="button" role="menuitem" onClick={requestExit}><AppIcon name="log-out" />退出游戏</button>
          </div> : null}
        </div>
      </div>
    </header>

    <aside id="player-debug-panel" className={`player-debug-panel${debugOpen ? " is-open" : ""}`} aria-label="运行调试信息" aria-hidden={!debugOpen}>
      <header><div><span>实时运行诊断</span><h2>调试信息</h2></div><button type="button" className="player-debug-close" aria-label="关闭调试信息面板" disabled={!debugOpen} onClick={onToggleDebug}><AppIcon name="x" /></button></header>
      <section><h3>实时</h3><dl>
        <div><dt>帧率</dt><dd>{debugMetrics?.fps === null || debugMetrics?.fps === undefined ? "采样中…" : `${debugMetrics.fps.toFixed(1)} FPS`}</dd></div>
        <div><dt>核心帧计数</dt><dd>{debugMetrics?.frameCount === null || debugMetrics?.frameCount === undefined ? "不可用" : debugMetrics.frameCount.toLocaleString("en-US")}</dd></div>
        <div><dt>运行状态</dt><dd>{runtimeState === "running" ? paused || netplayPaused ? "暂停" : "运行中" : runtimeState === "loading" ? "加载中" : "错误"}</dd></div>
        <div><dt>游戏分辨率</dt><dd>{debugMetrics?.canvasWidth && debugMetrics.canvasHeight ? `${debugMetrics.canvasWidth} × ${debugMetrics.canvasHeight}` : "等待画面"}</dd></div>
      </dl></section>
      <section><h3>运行环境</h3><dl>
        <div><dt>Core</dt><dd title={debugRuntime.coreId}>{coreName || debugRuntime.coreId || "—"}</dd></div>
        <div><dt>EmulatorJS</dt><dd>{debugRuntime.emulatorJSVersion || "—"}</dd></div>
        <div><dt>Player adapter</dt><dd title={debugRuntime.playerAdapterId}>{debugRuntime.playerAdapterId || "—"}</dd></div>
        <div><dt>输入模式</dt><dd>{debugRuntime.inputMode || "—"}</dd></div>
        <div><dt>隔离能力</dt><dd>{debugRuntime.crossOriginIsolated && debugRuntime.sharedArrayBuffer ? "COOP/COEP + SAB" : "未完整启用"}</dd></div>
        <div><dt>Player 模式</dt><dd>{isNetplay ? `联机 · P${netplayPlayerNo}` : "单机"}</dd></div>
      </dl></section>
      <section><h3>显示</h3><dl>
        <div><dt>视口</dt><dd>{debugMetrics ? `${debugMetrics.viewportWidth} × ${debugMetrics.viewportHeight}` : "—"}</dd></div>
        <div><dt>像素比</dt><dd>{debugMetrics ? debugMetrics.devicePixelRatio.toFixed(2) : "—"}</dd></div>
        {discSet && discState ? <div><dt>光盘</dt><dd>{discState.currentIndex + 1} / {discSet.count}</dd></div> : null}
      </dl></section>
      <footer title={debugRuntime.coreArtifactId}>Artifact · {debugRuntime.coreArtifactId || "等待配置"}</footer>
    </aside>

    <div className={`player-pause-overlay${paused || netplayPaused ? " is-visible" : ""}`} aria-hidden={!paused && !netplayPaused}>
      <div className="player-pause-pill"><AppIcon name="pause" /><strong>{isNetplay ? "联机已暂停" : "已暂停"}</strong><small>{isNetplay ? "等待房主继续" : "点击游戏画面继续"}</small></div>
    </div>

    {!isNetplay ? <section
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
    </section> : null}

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
      description={exitDescription}
      leadingLabel={isNetplay ? undefined : exitSaveState === "saved" ? "已创建存档" : exitSaveState === "error" ? "重试创建存档" : "创建存档"}
      leadingBusy={exitSaveState === "saving"}
      leadingBusyLabel="正在创建…"
      leadingDisabled={isNetplay || !running || exitSaveState === "saved"}
      confirmLabel="退出游戏"
      secondaryLabel={hasPersistentConflict ? "下载本地存档并退出" : undefined}
      tone="danger"
      onLeading={() => void createExitSave()}
      onCancel={() => setExitOpen(false)}
      onSecondary={hasPersistentConflict ? () => { onDownloadConflict(); setExitOpen(false); onExit(); } : undefined}
      onConfirm={() => { setExitOpen(false); onExit(); }}
    >
      {isNetplay ? <span>本局从头开始，退出后不会产生持久存档或状态存档。</span> : hasPersistentConflict ? <span>服务器存档已由另一会话推进；直接退出不会覆盖服务器版本，重新进入游戏后将从服务器最新版本开始。</span> : <span>退出时会刷新持久进度、结束本次游玩记录，并返回启动游戏前的页面。</span>}
    </ConfirmDialog>
  </>;
}
