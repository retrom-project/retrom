"use client";
import { useEffect, useState, type RefObject } from "react";
import { AppIcon } from "@/components/app-icon";
import type { LaunchEnvelopeV1, PlayerRuntimeV1, RuntimeStateV1 } from "./runtime/contract";
import { samplePlayerDebugMetrics, type PlayerDebugMetrics, type PlayerDebugSample } from "./player-debug";
import { PlayerInputDebug } from "./player-input-debug";
export function PlayerDebugPanel({ open, runtime, envelope, state, onClose }: {
  open: boolean;
  runtime: RefObject<PlayerRuntimeV1 | null>;
  envelope: LaunchEnvelopeV1;
  state: RuntimeStateV1;
  onClose: () => void;
}) {
  const [metrics, setMetrics] = useState<PlayerDebugMetrics | null>(null);
  useEffect(() => {
    if (!open) { return; }
    let previous: PlayerDebugSample | null = null;
    const sample = () => {
      const instance = runtime.current;
      let canvas: HTMLCanvasElement | null = null;
      try { canvas = instance?.getCanvas() ?? null; } catch { /* The frame may be closing. */ }
      const next = samplePlayerDebugMetrics(instance, canvas, previous, performance.now(), { width: innerWidth, height: innerHeight, devicePixelRatio });
      previous = next.sample;
      setMetrics(next.metrics);
    };
    const timer = window.setInterval(sample, 500);
    return () => window.clearInterval(timer);
  }, [runtime, open]);
  if (!open) { return null; }
  return <aside className="player-debug-panel is-open" aria-label="运行调试信息">
    <header><div><span>实时运行诊断</span><h2>调试信息</h2></div><button className="button ghost icon-only player-debug-close" aria-label="关闭调试信息" onClick={onClose}><AppIcon name="x" /></button></header>
    <PlayerInputDebug runtimeRef={runtime} ready={state === "RUNNING" || state === "PAUSED"} coreName={envelope.session.coreName} version={envelope.runtime.providerVersion} />
    <section><h3>实时</h3><dl>
      <div><dt>画面呈现率</dt><dd>{formatFps(metrics?.fps)}</dd></div>
      <div><dt>核心帧计数</dt><dd>{metrics?.frameCount ?? "不可用"}</dd></div>
      <div><dt>运行状态</dt><dd>{state === "PAUSED" ? "暂停" : state === "RUNNING" ? "运行中" : "加载中"}</dd></div>
      <div><dt>画布分辨率</dt><dd>{metrics?.canvasWidth && metrics.canvasHeight ? `${metrics.canvasWidth} × ${metrics.canvasHeight}` : "等待画面"}</dd></div>
    </dl><p className="player-debug-note">按核心提交的画面计算；静止或按需重绘时数值可能较低，不代表游戏运行速度。</p></section>
    <details className="player-debug-details"><summary>运行环境与显示</summary><section><dl><div><dt>运行核心</dt><dd>{envelope.session.coreName}</dd></div><div><dt>Provider</dt><dd>{envelope.runtime.providerId}</dd></div><div><dt>版本</dt><dd>{envelope.runtime.providerVersion}</dd></div><div><dt>视口</dt><dd>{metrics ? `${metrics.viewportWidth} × ${metrics.viewportHeight}` : "—"}</dd></div></dl></section></details>
  </aside>;
}

function formatFps(value: number | null | undefined) { return typeof value === "number" ? `${value.toFixed(1)} FPS` : "采样中…"; }
