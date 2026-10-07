"use client";
import { useRef } from "react";
import type {
  LaunchEnvelopeV1,
  RuntimeCheckpointAvailabilityV1,
  RuntimeStateV1,
} from "./runtime/contract";
import { AppIcon } from "@/components/app-icon";
import { PlayerFullscreenControl } from "./player-fullscreen-control";
import { PlayerHudHandle } from "./player-hud-handle";
export function PlayerToolbar({
  envelope,
  state,
  availability,
  busy,
  status,
  visible,
  onReveal,
  onHover,
  onFocus,
  onPause,
  onSave,
  onExit,
  onScreenshot,
  onSettings,
  onUseCover,
  settingsOpen,
  onVolume,
  onVideo,
  menu,
  onMenu,
  onControlError,
}: {
  envelope: LaunchEnvelopeV1;
  state: RuntimeStateV1;
  availability: RuntimeCheckpointAvailabilityV1;
  busy: boolean;
  status: string;
  visible: boolean;
  menu: boolean;
  onMenu: () => void;
  onControlError: (message: string) => void;
  onReveal: () => void;
  onHover: (value: boolean) => void;
  onFocus: (value: boolean) => void;
  onPause: () => void;
  onSave: () => void;
  onExit: () => void;
  onScreenshot: () => void;
  onSettings: () => void;
  onUseCover?: () => void;
  settingsOpen: boolean;
  onVolume: (value: number) => void;
  onVideo: (
    value: LaunchEnvelopeV1["runtime"]["capabilities"]["videoModes"][number],
  ) => void;
}) {
  const capabilities = envelope.runtime.capabilities;
  const controls = controlState(capabilities, state, availability, busy);
  const toolbarRef = useRef<HTMLElement>(null);
  return (
    <>
      <PlayerHudHandle
        visible={visible}
        toolbarRef={toolbarRef}
        onReveal={onReveal}
      />
      <header
        ref={toolbarRef}
        className={`player-toolbar${visible ? " is-visible" : ""}`}
        onPointerEnter={() => onHover(true)}
        onPointerLeave={() => onHover(false)}
        onFocusCapture={() => onFocus(true)}
        onBlurCapture={(event) => {
          if (!event.currentTarget.contains(event.relatedTarget)) {
            onFocus(false);
          }
        }}
      >
        <div className="player-game-meta">
          <strong>{envelope.session.title}</strong>
          <span>{envelope.session.coreName}</span>
        </div>
        <PlayerSyncStatus busy={busy} status={status} />
        <div className="player-actions">
          <button
            className="player-control is-icon"
            aria-label={state === "PAUSED" ? "继续" : "暂停"}
            disabled={controls.pauseDisabled}
            onClick={onPause}
          >
            <AppIcon name={state === "PAUSED" ? "play" : "pause"} />
          </button>
          <button
            className="player-control player-save-button"
            disabled={controls.saveDisabled}
            onClick={onSave}
          >
            <AppIcon name="save" />
            保存
          </button>
          <PlayerFullscreenControl onError={onControlError} />
          <div className="player-menu-wrap">
            <button
              className="player-control is-icon"
              aria-label="运行菜单"
              aria-expanded={menu}
              onClick={onMenu}
            >
              <AppIcon name="menu" />
            </button>
            {menu ? (
              <>
                <button
                  className="player-menu-backdrop"
                  aria-label="关闭运行菜单"
                  onClick={onMenu}
                />
                <div className="player-menu">
                  <header className="player-menu-head">
                    <div>
                      <small>Retrom</small>
                      <strong>运行菜单</strong>
                    </div>
                    <button aria-label="关闭运行菜单" onClick={onMenu}>
                      <AppIcon name="x" />
                    </button>
                  </header>
                  <div className="player-menu-runtime">
                    <i />
                    <span>
                      <strong>{envelope.session.title}</strong>
                      <small>{envelope.session.coreName}</small>
                    </span>
                  </div>
                  <button
                    aria-label="保存截图"
                    disabled={controls.screenshotDisabled}
                    onClick={onScreenshot}
                  >
                    <AppIcon name="download" />
                    <span>保存截图</span>
                  </button>
                  {onUseCover ? (
                    <button
                      aria-label="选用当前截图为封面"
                      disabled={controls.coverDisabled}
                      onClick={onUseCover}
                    >
                      <AppIcon name="library" />
                      <span>选用当前截图为封面</span>
                    </button>
                  ) : null}
                  <button
                    aria-label={settingsOpen ? "关闭运行设置" : "运行设置"}
                    disabled={controls.settingsDisabled}
                    onClick={onSettings}
                  >
                    <AppIcon name="settings" />
                    <span>{settingsOpen ? "关闭运行设置" : "运行设置"}</span>
                  </button>
                  {capabilities.volume ? (
                    <label className="field">
                      音量
                      <input
                        type="range"
                        disabled={!controls.ready}
                        min="0"
                        max="100"
                        defaultValue="100"
                        onChange={(event) =>
                          onVolume(Number(event.target.value) / 100)
                        }
                      />
                    </label>
                  ) : null}
                  {capabilities.videoModes.length ? (
                    <label className="field">
                      画面
                      <select
                        disabled={!controls.ready}
                        defaultValue={capabilities.videoModes[0]}
                        onChange={(event) =>
                          onVideo(
                            event.target
                              .value as (typeof capabilities.videoModes)[number],
                          )
                        }
                      >
                        {capabilities.videoModes.map((mode) => (
                          <option key={mode} value={mode}>
                            {videoLabels[mode]}
                          </option>
                        ))}
                      </select>
                    </label>
                  ) : null}
                  <hr />
                  <button
                    aria-label="退出游戏"
                    className="is-danger"
                    onClick={onExit}
                  >
                    <AppIcon name="log-out" />
                    <span>退出游戏</span>
                  </button>
                </div>
              </>
            ) : null}
          </div>
        </div>
      </header>
    </>
  );
}
function controlState(
  capabilities: LaunchEnvelopeV1["runtime"]["capabilities"],
  state: RuntimeStateV1,
  availability: RuntimeCheckpointAvailabilityV1,
  busy: boolean,
) {
  const ready = state === "RUNNING" || state === "PAUSED";
  return {
    ready,
    pauseDisabled: !ready || !capabilities.pause,
    saveDisabled: !ready || !availability.available || busy,
    screenshotDisabled: !ready || !capabilities.screenshot,
    coverDisabled: !ready || !capabilities.screenshot || busy,
    settingsDisabled: !ready || !capabilities.nativeSettings,
  };
}
function PlayerSyncStatus({ busy, status }: { busy: boolean; status: string }) {
  return (
    <div
      className={`player-sync-status${busy ? " is-busy" : ""}`}
      role="status"
      aria-label={busy ? "正在同步…" : status || "运行中"}
    >
      <i />
      <span>{busy ? "正在同步…" : status}</span>
    </div>
  );
}
const videoLabels = {
  original: "原始画面",
  pixel: "像素清晰",
  smooth: "平滑",
  "sharp-bilinear": "清晰双线性",
  "adaptive-sharpen": "自适应锐化",
};
