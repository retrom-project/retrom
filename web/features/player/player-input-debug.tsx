"use client";

import {useEffect, useRef, useState, type RefObject} from "react";
import type {PlayerRuntimeV1, RuntimeInputDiagnosticsSnapshotV1, RuntimeInputDiagnosticsV1, RuntimeInputObservationV1} from "./runtime/contract";

type Props = {runtimeRef?: RefObject<PlayerRuntimeV1 | null>; ready: boolean; coreName: string; version?: string};
const stages = {BROWSER: "浏览器检测", RUNTIME: "运行时取到", DELIVERED: "已投递"};

export function PlayerInputDebug({runtimeRef, ready, coreName, version}: Props) {
  const [snapshot, setSnapshot] = useState<RuntimeInputDiagnosticsSnapshotV1 | null>(null);
  const [notice, setNotice] = useState("");
  const sessionRef = useRef<RuntimeInputDiagnosticsV1 | null>(null);
  useEffect(() => {
    if (!ready) {return;}
    let session: RuntimeInputDiagnosticsV1 | undefined;
    try {session = runtimeRef?.current?.startInputDiagnostics?.();} catch {return;}
    if (!session) {return;}
    sessionRef.current = session;
    let signature = "";
    const sample = () => {
      if (document.hidden) {return;}
      try {
        const next = session.read();
        const nextSignature = `${next.events.at(-1)?.sequence}:${next.events.length}:${next.held.length}:${next.focus}`;
        if (signature === nextSignature) {return;}
        signature = nextSignature;
        setSnapshot(next);
      } catch { /* Optional diagnostics must not interrupt play. */ }
    };
    // Capture happens in the runtime input path. This timer only refreshes this small component.
    const timer = window.setInterval(sample, 100);
    return () => {window.clearInterval(timer); sessionRef.current = null; session.stop();};
  }, [runtimeRef, ready]);

  const clear = () => {sessionRef.current?.clear(); setNotice("");};
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(JSON.stringify({core: coreName, runtimeVersion: version, input: snapshot}, null, 2));
      setNotice("已复制");
    } catch {setNotice("复制失败，请重试");}
  };
  const latest = snapshot?.events.at(-1);
  return <section className="player-input-debug" aria-label="输入诊断">
    <h3>输入诊断</h3>
    <dl>
      <div><dt>键盘焦点</dt><dd>{focusLabel(snapshot)}</dd></div>
      <div><dt>按键观测</dt><dd>{snapshot?.keyboard ? "已开启" : "未接入"}</dd></div>
      <div><dt>手柄取样观测</dt><dd>{snapshot?.gamepad ? "已开启" : "未接入"}</dd></div>
      <div><dt>核心读取回执</dt><dd>未接入</dd></div>
    </dl>
    <div className="player-input-held" aria-label="当前按住的输入">
      {snapshot?.held.length ? snapshot.held.slice(0, 8).map((event) => <span key={event.sequence}>{inputLabel(event)}{event.control.startsWith("Axis") ? ` · ${event.value}` : ""}</span>) : <span className="is-neutral">当前未观测到按住的输入</span>}
    </div>
    <p className="player-input-latest">{latest ? describeInput(latest) : "按下手柄或键盘，查看输入到达的位置。"}</p>
    <p className="player-debug-note">已投递仅确认画布事件或输入接口调用，不代表游戏执行了动作。手柄仅观察运行时已有的取样；未显示不等于按键未按下。</p>
    <div className="player-input-actions" onPointerDown={(event) => event.preventDefault()}>
      <button type="button" onClick={() => void copy()} disabled={!snapshot}>复制记录</button>
      <button type="button" onClick={clear} disabled={!snapshot}>清空记录</button>
      <span role="status">{notice}</span>
    </div>
    <details className="player-input-records"><summary onPointerDown={(event) => event.preventDefault()}>最近输入记录</summary><ol className="player-input-history" aria-label="最近输入记录">
      {snapshot?.events.slice(-6).reverse().map((event) => <li key={event.sequence}>{describeInput(event)}</li>)}
    </ol></details>
    {snapshot && snapshot.dropped > 0 ? <p className="player-debug-note">仅保留最近 64 条变化，已省略 {snapshot.dropped} 条。</p> : null}
  </section>;
}

function focusLabel(snapshot: RuntimeInputDiagnosticsSnapshotV1 | null) {
  if (!snapshot?.keyboard) {return "无法观测";}
  return snapshot.focus === "GAME" ? "游戏画布" : snapshot.focus === "OTHER" ? "其他元素" : "游戏窗口未聚焦";
}

function inputLabel(event: RuntimeInputObservationV1) {
  const device = event.device === "gamepad" ? "手柄" : event.device.replace("keyboard", "键盘")
    .replace(/^gamepad:(\d+)$/, (_, index: string) => `手柄 ${Number(index) + 1}`)
    .replace(/^player:(\d+)$/, (_, index: string) => `P${Number(index) + 1}`);
  return `${device} · ${event.control}`;
}

export function describeInput(event: RuntimeInputObservationV1) {
  const action = event.value === 0 ? "松开" : event.control.startsWith("Axis") ? String(event.value) : "按下";
  const target = event.target ? ` → ${event.target}` : "";
  const duration = event.heldMs === null ? "" : ` · ${Math.round(event.heldMs)} ms`;
  const reason = event.reason === "INPUT_FILTER" ? " · 菜单/组合键过滤" : "";
  return `${inputLabel(event)} ${action}${target} · ${stages[event.stage]}${duration}${reason}`;
}
