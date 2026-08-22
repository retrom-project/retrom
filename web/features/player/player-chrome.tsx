"use client";

import { useEffect, useRef, useState, type Dispatch, type KeyboardEvent as ReactKeyboardEvent, type SetStateAction } from "react";
import { AppIcon } from "@/components/app-icon";
import { ConfirmDialog } from "@/components/confirm-dialog";
import type { EmulatorSettingsPanel } from "./emulator-settings";
import type { DiscSet, DiscState } from "./adapters/ejs-4.2.3-v2";
import { playerActionPriority } from "./player-actions";
import type { PlayerDebugMetrics } from "./player-debug";
import { videoRenderingModeOptions, type VideoRenderingMode } from "./video-rendering";

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

export type PlayerChromeProps = {
  controlsVisible: boolean; running: boolean; paused: boolean; fullscreen: boolean;
  gameTitle: string; coreName: string; platformName: string; syncText: string; syncTone: SyncTone;
  saveUploadProgress: number | null; saveAvailable: boolean; toast: string; warnings: string[];
  emulatorToolbarOpen: boolean; emulatorVolume: number; emulatorMuted: boolean; videoRenderingMode: VideoRenderingMode;
  discSet: DiscSet | null; discState: DiscState | null; netplayPlayerNo: number | null; netplayPaused: boolean;
  debugOpen: boolean; debugMetrics: PlayerDebugMetrics | null; debugRuntime: PlayerDebugRuntime; runtimeState: "loading" | "running" | "error";
  onHoldControls: () => void; onReleaseControls: () => void; onToggleControls: () => void; onSave: () => Promise<boolean>;
  onPauseForToolbarInteraction: () => void; onToggleFullscreen: () => void; onOpenEmulatorSettings: () => void;
  onCloseEmulatorSettings: () => void; onOpenEmulatorPanel: (panel: EmulatorSettingsPanel) => void;
  onChangeEmulatorVolume: (volume: number) => void; onToggleEmulatorMute: () => void;
  onChangeVideoRenderingMode: (mode: VideoRenderingMode) => void; onSelectDisc: (index: number) => Promise<boolean>;
  onToggleNetplayPause: () => void; onToggleDebug: () => void; onExit: () => void;
};

function exitDescriptionFor(netplay: boolean, saveAvailable: boolean, state: ExitSaveState) {
  if (netplay) {return "退出会结束所有参与者的本局联机，并返回房间。联机模式不会读取或写入个人存档。";}
  if (!saveAvailable) {return "当前从 DOS 程序菜单启动，无法创建可恢复存档；直接退出不会保存当前位置。";}
  if (state === "saving") {return "正在创建退出前存档…";}
  if (state === "saved") {return "退出前存档已创建，可以安全退出。";}
  if (state === "error") {return "创建存档失败，未生成不完整记录。可以重试或取消后继续游戏。";}
  return "直接退出不会创建存档；如需保留当前位置，请先创建存档。";
}

function warningCopyFor(warnings: string[]) {
  return warnings.includes("BIOS_HASH_WARNING")
    ? "BIOS 校验值与目录期望不同，但当前允许运行。"
    : "当前运行环境有需要留意的提示。";
}

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
  saveUploadProgress,
  saveAvailable,
  toast,
  warnings,
  emulatorToolbarOpen,
  emulatorVolume,
  emulatorMuted,
  videoRenderingMode,
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
  onToggleControls,
  onSave,
  onPauseForToolbarInteraction,
  onToggleFullscreen,
  onOpenEmulatorSettings,
  onCloseEmulatorSettings,
  onOpenEmulatorPanel,
  onChangeEmulatorVolume,
  onToggleEmulatorMute,
  onChangeVideoRenderingMode,
  onSelectDisc,
  onToggleNetplayPause,
  onToggleDebug,
  onExit,
}: PlayerChromeProps) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [discMenuOpen, setDiscMenuOpen] = useState(false);
  const [discBusy, setDiscBusy] = useState(false);
  const [exitOpen, setExitOpen] = useState(false);
  const [exitSaveState, setExitSaveState] = useState<ExitSaveState>("idle");
  const [localToast, setLocalToast] = useState("");
  const toolbarHovered = useRef(false);
  const toolbarFocused = useRef(false);
  const exitSavePending = useRef(false);

  useEffect(() => {
    if (!menuOpen) {return;}
    const menu = document.getElementById("player-more-menu");
    menu?.querySelector<HTMLElement>(".player-menu button:not(:disabled)")?.focus();
    const closeOnOutside = (event: PointerEvent) => {
      if (event.target instanceof Node && !menu?.contains(event.target)) {setMenuOpen(false);}
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {setMenuOpen(false); document.getElementById("player-more-button")?.focus();}
    };
    document.addEventListener("pointerdown", closeOnOutside);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("pointerdown", closeOnOutside);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [menuOpen]);

  useEffect(() => {
    if (!discMenuOpen) {return;}
    const menu = document.getElementById("player-disc-menu");
    const items = Array.from(menu?.querySelectorAll<HTMLButtonElement>("[role=\"menuitemradio\"]") ?? []);
    (items.find((item) => item.getAttribute("aria-checked") === "true") ?? items[0])?.focus();
    const close = (focusButton = false) => {
      setDiscMenuOpen(false);
      if (focusButton) {document.getElementById("player-disc-button")?.focus();}
    };
    const closeOnOutside = (event: PointerEvent) => {
      if (event.target instanceof Node && !menu?.contains(event.target)) {close();}
    };
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {close(true);}
    };
    document.addEventListener("pointerdown", closeOnOutside);
    document.addEventListener("keydown", closeOnEscape);
    return () => {
      document.removeEventListener("pointerdown", closeOnOutside);
      document.removeEventListener("keydown", closeOnEscape);
    };
  }, [discMenuOpen]);

  useEffect(() => {
    if (menuOpen || discMenuOpen || exitOpen || emulatorToolbarOpen || debugOpen) {onHoldControls();}
    else if (!toolbarHovered.current && !toolbarFocused.current) {onReleaseControls();}
  }, [debugOpen, discMenuOpen, emulatorToolbarOpen, exitOpen, menuOpen, onHoldControls, onReleaseControls]);

  useEffect(() => {
    if (!localToast) {return;}
    const timer = window.setTimeout(() => setLocalToast(""), 2_400);
    return () => window.clearTimeout(timer);
  }, [localToast]);

  const visibleToast = localToast || toast;
  const isNetplay = netplayPlayerNo !== null;
  const actionLayout = playerActionPriority({
    netplay: isNetplay,
    disc: !isNetplay && Boolean(discSet && discState),
    save: !isNetplay,
  });
  const warningCopy = warningCopyFor(warnings);

  function requestExit() {
    setMenuOpen(false);
    exitSavePending.current = false;
    setExitSaveState("idle");
    setExitOpen(true);
  }

  async function createExitSave() {
    if (isNetplay || !running || !saveAvailable || exitSavePending.current || exitSaveState === "saved") {return;}
    exitSavePending.current = true;
    onPauseForToolbarInteraction();
    setExitSaveState("saving");
    const saved = await onSave().catch(() => false);
    exitSavePending.current = false;
    setExitSaveState(saved ? "saved" : "error");
  }

  async function chooseDisc(index: number) {
    if (!discState || discBusy) {return;}
    if (index === discState.currentIndex) {
      setDiscMenuOpen(false);
      document.getElementById("player-disc-button")?.focus();
      return;
    }
    setDiscBusy(true);
    const changed = await onSelectDisc(index).catch(() => false);
    setDiscBusy(false);
    if (changed) {
      setDiscMenuOpen(false);
      document.getElementById("player-disc-button")?.focus();
    }
  }

  function moveDiscMenuFocus(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (!["ArrowDown", "ArrowRight", "ArrowUp", "ArrowLeft", "Home", "End"].includes(event.key)) {return;}
    const items = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>("[role=\"menuitemradio\"]:not(:disabled)"));
    if (!items.length) {return;}
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

  const exitDescription = exitDescriptionFor(isNetplay, saveAvailable, exitSaveState);

  return <>
    <SaveUploadProgress value={saveUploadProgress} />
    <button className="player-hud-handle" type="button" aria-label={controlsVisible ? "隐藏 Player 控制栏" : "显示 Player 控制栏"} aria-pressed={controlsVisible} onClick={onToggleControls}><span aria-hidden="true" /></button>
    <PlayerToolbar controlsVisible={controlsVisible} paused={paused} running={running} fullscreen={fullscreen} gameTitle={gameTitle} coreName={coreName} platformName={platformName} syncText={syncText} syncTone={syncTone} warnings={warnings} warningCopy={warningCopy} netplay={isNetplay} playerNo={netplayPlayerNo} netplayPaused={netplayPaused} saveAvailable={saveAvailable} actionLayout={actionLayout} debugOpen={debugOpen} discSet={discSet} discState={discState} discBusy={discBusy} discMenuOpen={discMenuOpen} menuOpen={menuOpen} blockingOverlay={exitOpen || emulatorToolbarOpen || debugOpen} onPause={onPauseForToolbarInteraction} onHold={onHoldControls} onRelease={onReleaseControls} onHover={(hovered) => {toolbarHovered.current = hovered;}} onFocus={(focused) => {toolbarFocused.current = focused;}} onExit={requestExit} onWarning={setLocalToast} onDebug={onToggleDebug} onSave={() => void onSave()} onToggleFullscreen={onToggleFullscreen} onToggleNetplayPause={onToggleNetplayPause} onChooseDisc={(index) => void chooseDisc(index)} onDiscMenu={setDiscMenuOpen} onDiscKey={moveDiscMenuFocus} onMenu={setMenuOpen} onEmulatorSettings={onOpenEmulatorSettings} />

    <PlayerDebugPanel open={debugOpen} metrics={debugMetrics} runtime={debugRuntime} runtimeState={runtimeState} paused={paused} netplayPaused={netplayPaused} coreName={coreName} playerNo={netplayPlayerNo} discSet={discSet} discState={discState} onClose={onToggleDebug} />

    <div className={`player-pause-overlay${paused || netplayPaused ? " is-visible" : ""}`} aria-hidden={!paused && !netplayPaused}>
      <div className="player-pause-pill"><AppIcon name="pause" /><strong>{isNetplay ? "联机已暂停" : "已暂停"}</strong><small>{isNetplay ? "等待房主继续" : "点击游戏画面继续"}</small></div>
    </div>

    {!isNetplay ? <EmulatorToolbar open={emulatorToolbarOpen} volume={emulatorVolume} muted={emulatorMuted} renderingMode={videoRenderingMode} onHold={onHoldControls} onOpenPanel={onOpenEmulatorPanel} onVolume={onChangeEmulatorVolume} onRenderingMode={onChangeVideoRenderingMode} onMute={onToggleEmulatorMute} onClose={onCloseEmulatorSettings} /> : null}

    <div className={`player-toast${visibleToast ? " is-visible" : ""}`} role="status" aria-live="polite">{visibleToast}</div>
    <div className={`player-controls-hint${controlsVisible ? " is-hidden" : ""}`}>移到屏幕顶部显示 Retrom 控制</div>

    <ExitGameDialog open={exitOpen} description={exitDescription} netplay={isNetplay} running={running} saveAvailable={saveAvailable} saveState={exitSaveState} onSave={() => void createExitSave()} onCancel={() => setExitOpen(false)} onConfirm={() => {setExitOpen(false); onExit();}} />
  </>;
}

function SaveUploadProgress({ value }: { value: number | null }) {
  if (value === null) {return null;}
  return <div className="player-save-upload-progress" role="status" aria-live="polite">
    <span>正在上传存档</span>
    <progress aria-label="存档上传进度" max="100" value={value} />
    <strong>{value}%</strong>
  </div>;
}

type ToolbarProps = {
  controlsVisible: boolean; paused: boolean; running: boolean; fullscreen: boolean; gameTitle: string; coreName: string; platformName: string;
  syncText: string; syncTone: SyncTone; warnings: string[]; warningCopy: string; netplay: boolean; playerNo: number | null; netplayPaused: boolean;
  saveAvailable: boolean; actionLayout: ReturnType<typeof playerActionPriority>; debugOpen: boolean; discSet: DiscSet | null; discState: DiscState | null;
  discBusy: boolean; discMenuOpen: boolean; menuOpen: boolean; blockingOverlay: boolean;
  onHover: (hovered: boolean) => void; onFocus: (focused: boolean) => void;
  onPause: () => void; onHold: () => void; onRelease: () => void; onExit: () => void; onWarning: (message: string) => void; onDebug: () => void; onSave: () => void;
  onToggleFullscreen: () => void; onToggleNetplayPause: () => void; onChooseDisc: (index: number) => void; onDiscMenu: Dispatch<SetStateAction<boolean>>;
  onDiscKey: (event: ReactKeyboardEvent<HTMLDivElement>) => void; onMenu: Dispatch<SetStateAction<boolean>>; onEmulatorSettings: () => void;
};

function DiscControl({ props }: { props: ToolbarProps }) {
  if (props.netplay || !props.discSet || !props.discState) {return null;}
  return <div id="player-disc-menu" className="player-menu-wrap player-disc-wrap"><button id="player-disc-button" className={`player-control player-disc-button player-context-action${props.actionLayout.primary === "disc" ? " is-primary" : ""}`} type="button" disabled={!props.running || props.discBusy} aria-label={`光盘 ${props.discState.currentIndex + 1} / ${props.discSet.count}`} aria-expanded={props.discMenuOpen} aria-haspopup="menu" onClick={() => props.onDiscMenu((open) => !open)}><span aria-hidden="true">◉</span>光盘 {props.discState.currentIndex + 1} / {props.discSet.count}</button>{props.discMenuOpen ? <><button className="player-menu-backdrop" type="button" tabIndex={-1} aria-label="关闭光盘选择" onClick={() => props.onDiscMenu(false)} /><div className="player-menu player-disc-menu" role="menu" aria-label="选择光盘" onKeyDown={props.onDiscKey}><strong>选择光盘</strong>{props.discSet.entries.map((entry) => <button key={entry.index} type="button" role="menuitemradio" aria-checked={entry.index === props.discState!.currentIndex} disabled={props.discBusy} onClick={() => props.onChooseDisc(entry.index)}><span aria-hidden="true">{entry.index === props.discState!.currentIndex ? "✓" : "○"}</span>{entry.label}{entry.index === props.discState!.currentIndex ? " · 当前" : ""}</button>)}<small>切换后游戏保持暂停，返回游戏即可继续。</small></div></> : null}</div>;
}

function MoreActions({ props }: { props: ToolbarProps }) {
  return <div id="player-more-menu" className="player-menu-wrap"><button id="player-more-button" className="player-control is-icon" type="button" aria-label="更多操作" title="更多操作" aria-expanded={props.menuOpen} aria-haspopup="menu" onClick={() => props.onMenu((open) => !open)}><AppIcon name="more" /></button>{props.menuOpen ? <><button className="player-menu-backdrop" type="button" tabIndex={-1} aria-label="关闭更多操作" onClick={() => props.onMenu(false)} /><div className="player-menu" role="menu" aria-label="Player 更多操作"><header className="player-menu-head"><div><small>Retrom Player</small><strong>更多操作</strong></div><button type="button" aria-label="关闭更多操作" onClick={() => {props.onMenu(false); document.getElementById("player-more-button")?.focus();}}><AppIcon name="x" /></button></header><div className={`player-menu-runtime is-${props.syncTone}`} role="status"><i aria-hidden="true" /><span><strong>{props.syncText}</strong><small>{props.netplay ? `联机座位 P${props.playerNo}` : props.paused ? "当前已暂停" : "游戏运行中"}</small></span></div>{!props.netplay && props.discSet && props.discState ? <button type="button" role="menuitem" aria-label="在更多操作中选择光盘" disabled={!props.running || props.discBusy} onClick={() => {props.onMenu(false); props.onDiscMenu(true);}}><AppIcon name="database" /><span><strong>光盘 {props.discState.currentIndex + 1} / {props.discSet.count}</strong><small>选择当前运行光盘</small></span></button> : null}{!props.netplay ? <button type="button" role="menuitem" onClick={() => {props.onMenu(false); props.onEmulatorSettings();}}><AppIcon name="settings" />模拟器设置</button> : null}<button type="button" role="menuitem" aria-label={props.fullscreen ? "在更多操作中退出全屏" : "在更多操作中进入全屏"} onClick={() => {props.onMenu(false); props.onToggleFullscreen();}}><AppIcon name={props.fullscreen ? "minimize" : "maximize"} />{props.fullscreen ? "退出全屏" : "进入全屏"}</button><button type="button" role="menuitem" aria-label="在更多操作中打开调试信息" onClick={() => {props.onMenu(false); props.onDebug();}}><AppIcon name="chip" />调试信息</button><button className="player-shortcut-action" type="button" role="menuitem" onClick={() => {props.onMenu(false); props.onWarning("将鼠标移到屏幕顶部可显示控制栏；Esc 只退出浏览器全屏，不会退出游戏。");}}><AppIcon name="keyboard" />查看快捷键</button>{props.warnings.length ? <button type="button" role="menuitem" onClick={() => {props.onMenu(false); props.onWarning(props.warningCopy);}}><AppIcon name="warning" />查看运行提醒</button> : null}<hr /><button className="is-danger" type="button" role="menuitem" onClick={props.onExit}><AppIcon name="log-out" />退出游戏</button></div></> : null}</div>;
}

function ToolbarActions({ props }: { props: ToolbarProps }) {
  return <div className="player-actions"><button className="player-control player-debug-control player-mobile-overflow" type="button" aria-expanded={props.debugOpen} aria-controls="player-debug-panel" aria-pressed={props.debugOpen} onClick={props.onDebug}><AppIcon name="chip" />调试信息</button><DiscControl props={props} /><PlayerContextActions props={props} /><button className="player-control is-icon player-mobile-overflow" type="button" aria-label={props.fullscreen ? "退出全屏" : "全屏"} title={props.fullscreen ? "退出全屏" : "全屏"} onClick={props.onToggleFullscreen}><AppIcon name={props.fullscreen ? "minimize" : "maximize"} /></button><MoreActions props={props} /></div>;
}

function PlayerContextActions({ props }: { props: ToolbarProps }) {
  if (!props.netplay) {return <><button className={`player-control player-save-button player-context-action${props.actionLayout.primary === "save" ? " is-primary" : ""}`} type="button" disabled={!props.running || !props.saveAvailable} title={!props.saveAvailable ? "请退出后从游戏详情选择具体 DOS 程序再开始" : undefined} onClick={props.onSave}><AppIcon name="save" />创建存档</button><button className="player-control is-icon" type="button" aria-label={props.paused ? "已暂停，点击游戏画面继续" : "暂停"} title={props.paused ? "点击游戏画面继续" : "暂停"} aria-pressed={props.paused} disabled={!props.running}><AppIcon name="pause" /></button></>;}
  if (props.playerNo === 1) {return <button className="player-control player-context-action is-primary" type="button" disabled={!props.running} aria-pressed={props.netplayPaused} onClick={props.onToggleNetplayPause}><AppIcon name={props.netplayPaused ? "play" : "pause"} />{props.netplayPaused ? "继续联机" : "全局暂停"}</button>;}
  return <span className="player-seat-context player-context-action is-primary">联机 · P{props.playerNo}</span>;
}

function PlayerToolbar(props: ToolbarProps) {
  const releaseIfClear = () => {
    if (!props.menuOpen && !props.discMenuOpen && !props.blockingOverlay) {props.onRelease();}
  };
  return <header className={`player-toolbar${props.controlsVisible || props.paused ? " is-visible" : ""}`} onClickCapture={(event) => {if (!(event.target instanceof Element && event.target.closest(".player-disc-wrap,.player-debug-control"))) {props.onPause();}}} onBlurCapture={(event) => {if (!(event.relatedTarget instanceof Node && event.currentTarget.contains(event.relatedTarget))) {props.onFocus(false); releaseIfClear();}}} onFocusCapture={() => {props.onFocus(true); props.onHold();}} onPointerEnter={() => {props.onHover(true); props.onHold();}} onPointerLeave={() => {props.onHover(false); releaseIfClear();}} onPointerMove={(event) => event.stopPropagation()}><button className="player-back" type="button" aria-label="返回并退出游戏" title="返回并退出游戏" onClick={props.onExit}><AppIcon name="arrow-left" /></button><div className="player-game-meta"><strong>{props.gameTitle}</strong><span>{[props.coreName, props.platformName, props.netplay ? `联机 · P${props.playerNo}` : ""].filter(Boolean).join(" · ")}</span></div><div className={`player-sync-status is-${props.syncTone}`} role="status" aria-live="polite"><i aria-hidden="true" /><span>{props.syncText}</span>{props.warnings.length ? <button className="player-warning-dot" type="button" aria-label="查看运行提醒" title="查看运行提醒" onClick={() => props.onWarning(props.warningCopy)} /> : null}</div><ToolbarActions props={props} /></header>;
}

function PlayerDebugPanel({ open, metrics, runtime, runtimeState, paused, netplayPaused, coreName, playerNo, discSet, discState, onClose }: { open: boolean; metrics: PlayerDebugMetrics | null; runtime: PlayerDebugRuntime; runtimeState: "loading" | "running" | "error"; paused: boolean; netplayPaused: boolean; coreName: string; playerNo: number | null; discSet: DiscSet | null; discState: DiscState | null; onClose: () => void }) {
  const runningLabel = runtimeState === "running" ? paused || netplayPaused ? "暂停" : "运行中" : runtimeState === "loading" ? "加载中" : "错误";
  return <aside id="player-debug-panel" className={`player-debug-panel${open ? " is-open" : ""}`} aria-label="运行调试信息" aria-hidden={!open}><header><div><span>实时运行诊断</span><h2>调试信息</h2></div><button type="button" className="player-debug-close" aria-label="关闭调试信息面板" disabled={!open} onClick={onClose}><AppIcon name="x" /></button></header><LiveDebug metrics={metrics} runningLabel={runningLabel} /><RuntimeDebug runtime={runtime} coreName={coreName} playerNo={playerNo} /><DisplayDebug metrics={metrics} discSet={discSet} discState={discState} /><footer title={runtime.coreArtifactId}>Artifact · {runtime.coreArtifactId || "等待配置"}</footer></aside>;
}

function LiveDebug({ metrics, runningLabel }: { metrics: PlayerDebugMetrics | null; runningLabel: string }) {
  return <section><h3>实时</h3><dl><div><dt>帧率</dt><dd>{metrics?.fps === null || metrics?.fps === undefined ? "采样中…" : `${metrics.fps.toFixed(1)} FPS`}</dd></div><div><dt>核心帧计数</dt><dd>{metrics?.frameCount === null || metrics?.frameCount === undefined ? "不可用" : metrics.frameCount.toLocaleString("en-US")}</dd></div><div><dt>运行状态</dt><dd>{runningLabel}</dd></div><div><dt>游戏分辨率</dt><dd>{metrics?.canvasWidth && metrics.canvasHeight ? `${metrics.canvasWidth} × ${metrics.canvasHeight}` : "等待画面"}</dd></div></dl></section>;
}

function RuntimeDebug({ runtime, coreName, playerNo }: { runtime: PlayerDebugRuntime; coreName: string; playerNo: number | null }) {
  return <section><h3>运行环境</h3><dl><div><dt>Core</dt><dd title={runtime.coreId}>{coreName || runtime.coreId || "—"}</dd></div><div><dt>EmulatorJS</dt><dd>{runtime.emulatorJSVersion || "—"}</dd></div><div><dt>Player adapter</dt><dd title={runtime.playerAdapterId}>{runtime.playerAdapterId || "—"}</dd></div><div><dt>输入模式</dt><dd>{runtime.inputMode || "—"}</dd></div><div><dt>隔离能力</dt><dd>{runtime.crossOriginIsolated && runtime.sharedArrayBuffer ? "COOP/COEP + SAB" : "未完整启用"}</dd></div><div><dt>Player 模式</dt><dd>{playerNo === null ? "单机" : `联机 · P${playerNo}`}</dd></div></dl></section>;
}

function DisplayDebug({ metrics, discSet, discState }: { metrics: PlayerDebugMetrics | null; discSet: DiscSet | null; discState: DiscState | null }) {
  return <section><h3>显示</h3><dl><div><dt>视口</dt><dd>{metrics ? `${metrics.viewportWidth} × ${metrics.viewportHeight}` : "—"}</dd></div><div><dt>像素比</dt><dd>{metrics ? metrics.devicePixelRatio.toFixed(2) : "—"}</dd></div>{discSet && discState ? <div><dt>光盘</dt><dd>{discState.currentIndex + 1} / {discSet.count}</dd></div> : null}</dl></section>;
}

function EmulatorToolbar({ open, volume, muted, renderingMode, onHold, onOpenPanel, onVolume, onRenderingMode, onMute, onClose }: { open: boolean; volume: number; muted: boolean; renderingMode: VideoRenderingMode; onHold: () => void; onOpenPanel: (panel: EmulatorSettingsPanel) => void; onVolume: (volume: number) => void; onRenderingMode: (mode: VideoRenderingMode) => void; onMute: () => void; onClose: () => void }) {
  const volumePercent = Math.round(volume * 100);
  return <section className={`player-emulator-toolbar${open ? " is-open" : ""}`} aria-label="模拟器设置工具栏" aria-hidden={!open} onFocusCapture={onHold} onPointerEnter={onHold}><div className="player-emulator-group"><span className="player-emulator-label">模拟器</span><button type="button" disabled={!open} onClick={() => onOpenPanel("controls")}><AppIcon name="gamepad" />控制</button><button type="button" disabled={!open} onClick={() => onOpenPanel("display")}><span aria-hidden="true">▤</span>显示</button><button type="button" disabled={!open} onClick={() => onOpenPanel("core")}><span aria-hidden="true">⚙</span>Core 设置</button></div><label className="player-emulator-rendering"><span className="player-emulator-label">画面</span><select aria-label="画面模式" disabled={!open} value={renderingMode} onChange={(event) => onRenderingMode(event.currentTarget.value as VideoRenderingMode)}>{videoRenderingModeOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label><div className="player-emulator-group"><label className="player-emulator-volume"><span className="player-emulator-label">音量</span><input type="range" min="0" max="100" step="1" value={volumePercent} aria-label="模拟器音量" aria-valuetext={muted ? `已静音，音量 ${volumePercent}%` : `${volumePercent}%`} disabled={!open} onChange={(event) => onVolume(Number(event.currentTarget.value) / 100)} /></label><button type="button" disabled={!open} aria-label={muted ? "取消静音" : "静音"} aria-pressed={muted} onClick={onMute}><span aria-hidden="true">{muted ? "🔇" : "🔊"}</span></button><button type="button" disabled={!open} onClick={onClose}>收起</button></div></section>;
}

function ExitGameDialog({ open, description, netplay, running, saveAvailable, saveState, onSave, onCancel, onConfirm }: { open: boolean; description: string; netplay: boolean; running: boolean; saveAvailable: boolean; saveState: ExitSaveState; onSave: () => void; onCancel: () => void; onConfirm: () => void }) {
  const leadingLabel = saveState === "saved" ? "已创建存档" : saveState === "error" ? "重试创建存档" : "创建存档";
  return <ConfirmDialog open={open} title="退出游戏？" description={description} leadingLabel={netplay ? undefined : leadingLabel} leadingBusy={saveState === "saving"} leadingBusyLabel="正在创建…" leadingDisabled={netplay || !running || !saveAvailable || saveState === "saved"} confirmLabel="退出游戏" tone="danger" onLeading={onSave} onCancel={onCancel} onConfirm={onConfirm}>{netplay ? <span>本局从头开始，退出后不会产生状态存档。</span> : saveAvailable ? <span>只有点击“创建存档”才会保存当前位置；直接退出只结束本次游玩记录。</span> : <span>请退出后从游戏详情选择一个具体 DOS 程序再开始，届时即可创建并恢复存档。</span>}</ConfirmDialog>;
}
