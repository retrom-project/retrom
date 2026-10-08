"use client";
import { useRef } from "react";
import type { LaunchEnvelopeV1, RuntimeCheckpointAvailabilityV1, RuntimeStateV1 } from "./runtime/contract";
import { AppIcon } from "@/components/app-icon";
import { PlayerFullscreenControl } from "./player-fullscreen-control";
import { PlayerHudHandle } from "./player-hud-handle";
import { useMobilePlayerLayout } from "./player-layout";

type Props = {
  immersive?: boolean;
  envelope: LaunchEnvelopeV1;
  state: RuntimeStateV1;
  availability: RuntimeCheckpointAvailabilityV1;
  busy: boolean;
  status: string;
  visible: boolean;
  menu: boolean;
  debugOpen: boolean;
  onDebug: () => void;
  onMenu: () => void;
  onControlError: (message: string) => void;
  onReveal: () => void;
  onHover: (value: boolean) => void;
  onFocus: (value: boolean) => void;
  onPause: () => void;
  onSave: () => void;
  onExit: () => void;
  onSettings: () => void;
  onGameEditor?: () => void;
  onUseCover?: () => void;
};
export function PlayerToolbar(props: Props) {
  const { envelope, state, availability, busy, visible, onReveal, onHover, onFocus, onPause, onSave, onExit, onControlError } = props;
  const controls = controlState(envelope.runtime.capabilities, state, availability, busy);
  const toolbarRef = useRef<HTMLElement>(null);
  const mobile = useMobilePlayerLayout();
  if (props.immersive) { return null; }
  return <>
    <PlayerHudHandle visible={visible} toolbarRef={toolbarRef} onReveal={onReveal} />
    <header ref={toolbarRef} className={`player-toolbar${visible ? " is-visible" : ""}`} onPointerEnter={() => onHover(true)} onPointerLeave={() => onHover(false)} onFocusCapture={() => onFocus(true)} onBlurCapture={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) { onFocus(false); } }}>
      <button className="player-back button ghost icon-only" aria-label="返回并退出游戏" onClick={onExit}><AppIcon name="arrow-left" /></button>
      <PlayerSyncStatus {...props} />
      <div className="player-actions">
        {!mobile ? <button className="player-control player-debug-control" aria-expanded={props.debugOpen} onClick={props.onDebug}><AppIcon name="chip" />调试信息</button> : null}
        <button className="player-control player-save-button player-context-action is-primary" disabled={controls.saveDisabled} onClick={onSave}><AppIcon name="save" />{envelope.runtime.checkpoint?.semantics === "GAME_SAVE" ? "同步存档" : mobile ? "保存" : "创建存档"}</button>
        <button className="player-control is-icon" aria-label={state === "PAUSED" ? "继续" : "暂停"} aria-pressed={state === "PAUSED"} disabled={controls.pauseDisabled} onClick={onPause}><AppIcon name={state === "PAUSED" ? "play" : "pause"} /></button>
        {!mobile ? <PlayerFullscreenControl onError={onControlError} /> : null}
        <div className="player-menu-wrap">
          <button id="player-more-button" className="player-control is-icon" aria-label="更多操作" aria-haspopup="menu" aria-expanded={props.menu} onClick={props.onMenu}><AppIcon name="more" /></button>
          {props.menu ? <PlayerMenu {...props} controls={controls} /> : null}
        </div>
      </div>
    </header>
  </>;
}
function PlayerMenu(props: Props & { controls: ReturnType<typeof controlState> }) {
  const { controls, onMenu, onSettings, onUseCover, onExit, onControlError } = props;
  return <>
    <button className="player-menu-backdrop" tabIndex={-1} aria-label="关闭更多操作" onClick={onMenu} />
    <div className="player-menu" role="menu" aria-label="Player 更多操作">
      <header className="player-menu-head"><div><small>Retrom Player</small><strong>更多操作</strong></div><button aria-label="关闭更多操作" onClick={onMenu}><AppIcon name="x" /></button></header>
      <div className="player-menu-runtime"><i /><span><strong>{props.status || checkpointStatus(props.envelope, props.availability)}</strong><small>{props.state === "PAUSED" ? "当前已暂停" : props.envelope.session.coreName}</small></span></div>
      {onUseCover ? <button role="menuitem" aria-label="选用当前截图为封面" disabled={controls.coverDisabled} onClick={onUseCover}><AppIcon name="library" /><span>选用当前截图为封面</span></button> : null}
      {props.onGameEditor ? <button role="menuitem" disabled={!controls.ready} onClick={props.onGameEditor}><AppIcon name="settings" /><span>游戏修改</span></button> : null}
      {controls.settingsAvailable ? <button role="menuitem" aria-label="模拟器设置" disabled={!controls.ready} onClick={onSettings}><AppIcon name="settings" /><span>模拟器设置</span></button> : null}
      <PlayerFullscreenControl menu onError={onControlError} />
      <hr />
      <button role="menuitem" aria-label="退出游戏" className="is-danger" onClick={onExit}><AppIcon name="log-out" /><span>退出游戏</span></button>
    </div>
  </>;
}
function controlState(capabilities: LaunchEnvelopeV1["runtime"]["capabilities"], state: RuntimeStateV1, availability: RuntimeCheckpointAvailabilityV1, busy: boolean) {
  const ready = state === "RUNNING" || state === "PAUSED";
  return {
    ready,
    pauseDisabled: !ready || !capabilities.pause,
    saveDisabled: !ready || !availability.available || busy,
    coverDisabled: !ready || !capabilities.screenshot || busy,
    settingsAvailable: capabilities.nativeSettings || capabilities.volume || capabilities.videoModes.length > 0,
  };
}
function PlayerSyncStatus({ busy, status, state, availability, envelope }: Pick<Props, "busy" | "status" | "state" | "availability" | "envelope">) {
  const starting = state === "CREATED" || state === "MOUNTING";
  const label = busy ? "正在同步…" : status || (starting ? "正在连接…" : checkpointStatus(envelope, availability));
  return <div className={`player-sync-status${busy || starting ? " is-busy" : ""}`} role="status" aria-label={label}><i /><span>{label}</span></div>;
}

function checkpointStatus(envelope: LaunchEnvelopeV1, availability: RuntimeCheckpointAvailabilityV1) {
  if (!availability.available) { return "游戏运行中"; }
  return envelope.runtime.checkpoint?.semantics === "GAME_SAVE" ? "有待同步的游戏数据" : "可创建存档";
}
