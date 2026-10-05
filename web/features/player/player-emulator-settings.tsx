"use client";

import {useLayoutEffect, useRef, useState, useSyncExternalStore, type KeyboardEvent, type RefObject} from "react";
import {AppIcon} from "@/components/app-icon";
import type {EmulatorSettingsPanel} from "./emulator-settings";
import type {PlayerRuntimeV1, RuntimeCapabilitiesV1} from "./runtime/contract";
import {videoRenderingModeOptions, type VideoRenderingMode} from "./video-rendering";

export type EmulatorSettingsCapabilities = Pick<RuntimeCapabilitiesV1, "nativeSettings" | "volume" | "videoModes" | "standardGamepad">;

export function hasEmulatorSettings(capabilities?: EmulatorSettingsCapabilities) {
  return Boolean(capabilities && (capabilities.nativeSettings || capabilities.volume || capabilities.videoModes.length));
}

// Capabilities are immutable for a runtime instance; Player's readiness transition
// prompts a new snapshot after bootstrap has installed that instance.
function subscribeCapabilities() {return () => {};}
export function useEmulatorSettingsCapabilities(override: EmulatorSettingsCapabilities | undefined, runtime: RefObject<PlayerRuntimeV1 | null> | undefined, ready: boolean) {
  return useSyncExternalStore(subscribeCapabilities,
    () => override ?? (ready ? runtime?.current?.getCapabilities() : undefined), () => undefined);
}

function subscribeGamepads(onChange: () => void) {
  window.addEventListener("gamepadconnected", onChange);
  window.addEventListener("gamepaddisconnected", onChange);
  return () => {
    window.removeEventListener("gamepadconnected", onChange);
    window.removeEventListener("gamepaddisconnected", onChange);
  };
}

function gamepadConnected() {
  return Boolean(navigator.getGamepads?.().some((gamepad) => gamepad?.connected));
}

type Props = {
  mobile: boolean;
  capabilities: EmulatorSettingsCapabilities;
  volume: number; muted: boolean; renderingMode: VideoRenderingMode;
  onHold: () => void;
  onOpenPanel: (panel: EmulatorSettingsPanel | null) => Promise<boolean>;
  onClose: () => Promise<boolean>;
  onVolume: (volume: number) => void;
  onRenderingMode: (mode: VideoRenderingMode) => void;
  onMute: () => void;
};

function useSettingsNavigation({onOpenPanel, onClose}: Pick<Props, "onOpenPanel" | "onClose">) {
  const [panel, setPanel] = useState<EmulatorSettingsPanel | null>(null);
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const returnPanel = useRef<EmulatorSettingsPanel | null>(null);
  const container = useRef<HTMLElement>(null);

  useLayoutEffect(() => {
    const target = !panel && returnPanel.current
      ? container.current?.querySelector<HTMLButtonElement>(`[data-native-panel="${returnPanel.current}"]`)
      : container.current?.querySelector<HTMLButtonElement>("button");
    target?.focus({preventScroll: true});
  }, [panel]);

  async function navigate(next: EmulatorSettingsPanel | null | "close") {
    if (pending.current) {return;}
    pending.current = true;
    setBusy(true);
    try {
      const changed = await (next === "close" ? onClose() : onOpenPanel(next));
      if (!changed) {return;}
      if (next === "close") {requestAnimationFrame(() => document.getElementById("player-more-button")?.focus({preventScroll: true}));}
      else {
        if (next) {returnPanel.current = next;}
        setPanel(next);
      }
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }

  function onKeyDown(event: KeyboardEvent) {
    if (event.key !== "Escape") {return;}
    event.preventDefault();
    event.stopPropagation();
    void navigate(panel ? null : "close");
  }
  return {panel, busy, container, navigate, onKeyDown};
}

export function EmulatorSettingsLayer({open, capabilities, ...props}: Omit<Props, "capabilities"> & {open: boolean; capabilities?: EmulatorSettingsCapabilities}) {
  return open && capabilities ? <PlayerEmulatorSettings {...props} capabilities={capabilities} /> : null;
}

function PictureSetting({capabilities, renderingMode, onRenderingMode, busy}: Props & {busy: boolean}) {
  const modes = videoRenderingModeOptions.filter((option) => capabilities.videoModes.includes(option.value));
  if (!modes.length) {return null;}
  const selected = modes.some((option) => option.value === renderingMode) ? renderingMode : modes[0].value;
  return <label className="player-emulator-rendering"><span className="player-emulator-label">画面</span><select aria-label="画面模式" disabled={busy} value={selected} onChange={(event) => onRenderingMode(event.currentTarget.value as VideoRenderingMode)}>{modes.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label>;
}

function AudioSetting({capabilities, volume, muted, onVolume, onMute, busy}: Props & {busy: boolean}) {
  if (!capabilities.volume) {return null;}
  const volumePercent = Math.round(volume * 100);
  return <div className="player-emulator-audio"><label className="player-emulator-volume"><span className="player-emulator-label">音量</span><input type="range" min="0" max="100" step="1" value={volumePercent} aria-label="模拟器音量" aria-valuetext={muted ? `已静音，音量 ${volumePercent}%` : `${volumePercent}%`} disabled={busy} onChange={(event) => onVolume(Number(event.currentTarget.value) / 100)} /><output>{volumePercent}%</output></label><button type="button" disabled={busy} aria-label={muted ? "取消静音" : "静音"} aria-pressed={muted} onClick={onMute}>{muted ? "取消静音" : "静音"}</button></div>;
}

type Navigation = ReturnType<typeof useSettingsNavigation>;
function NativeOptions({mobile, capabilities, navigation, advanced, onAdvanced}: Pick<Props, "mobile" | "capabilities"> & {navigation: Navigation; advanced: boolean; onAdvanced: () => void}) {
  const connected = useSyncExternalStore(subscribeGamepads, gamepadConnected, () => false);
  if (!capabilities.nativeSettings) {return null;}
  const {busy, navigate} = navigation;
  const controls = !mobile || (connected && capabilities.standardGamepad);
  return <div className="player-emulator-advanced">
    {mobile ? <button className="player-settings-advanced-toggle" type="button" disabled={busy} aria-expanded={advanced} aria-controls="player-native-options" onClick={onAdvanced}><AppIcon name="settings" />高级设置<span aria-hidden="true">{advanced ? "−" : "+"}</span></button> : null}
    {!mobile || advanced ? <div className="player-emulator-group" id="player-native-options">
      {controls ? <button type="button" data-native-panel="controls" disabled={busy} onClick={() => void navigate("controls")}><AppIcon name="gamepad" />控制</button> : null}
      <button type="button" data-native-panel="display" disabled={busy} onClick={() => void navigate("display")}><AppIcon name="maximize" />显示</button>
      <button type="button" data-native-panel="core" disabled={busy} onClick={() => void navigate("core")}><AppIcon name="chip" />Core 设置</button>
    </div> : null}
  </div>;
}

export function PlayerEmulatorSettings(props: Props) {
  const {mobile, capabilities, onHold} = props;
  const navigation = useSettingsNavigation(props);
  const {panel, busy, container, navigate, onKeyDown} = navigation;
  const [advanced, setAdvanced] = useState(false);
  const closeButton = <button type="button" disabled={busy} onClick={() => void navigate("close")}>收起</button>;

  if (panel) {
    return <section ref={container} className="player-native-settings" aria-label="原生设置导航" aria-busy={busy} onKeyDown={onKeyDown}>
      <button className="button ghost icon-only" type="button" disabled={busy} aria-label="返回设置" title="返回设置" onClick={() => void navigate(null)}><AppIcon name="arrow-left" /></button>
      <strong>{panel === "display" ? "显示" : panel === "core" ? "Core 设置" : "控制"}</strong>
      <button className="button ghost icon-only" type="button" disabled={busy} title="关闭模拟器设置" aria-label="关闭模拟器设置" onClick={() => void navigate("close")}><AppIcon name="x" /></button>
    </section>;
  }

  return <>
    {mobile ? <button type="button" className="player-settings-backdrop" tabIndex={-1} disabled={busy} aria-label="关闭模拟器设置" onClick={() => void navigate("close")} /> : null}
    <section ref={container} className="player-emulator-toolbar is-open" aria-label="模拟器设置工具栏" aria-busy={busy} onKeyDown={onKeyDown} onFocusCapture={onHold} onPointerEnter={onHold}>
      {mobile ? <header className="player-settings-heading"><strong>模拟器设置</strong>{closeButton}</header> : null}
      <PictureSetting {...props} busy={busy} />
      <AudioSetting {...props} busy={busy} />
      <NativeOptions mobile={mobile} capabilities={capabilities} navigation={navigation} advanced={advanced} onAdvanced={() => setAdvanced((value) => !value)} />
      {!mobile ? closeButton : null}
    </section>
  </>;
}
