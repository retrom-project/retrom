"use client";

import {useLayoutEffect, useRef, type CSSProperties} from "react";
import type {RuntimeStartupTaskV1} from "./runtime/contract";
import {useStartupHistoryMotion} from "./startup-task-motion";
import {startupTaskNames, startupPercentage} from "./startup-task-model";

export function PlayerStartupTasks({tasks}: {tasks: RuntimeStartupTaskV1[]}) {
  const ref = useStartupHistoryMotion(tasks);
  return <div className="player-startup-viewport"><ol ref={ref} className="player-startup-tasks" aria-label="游戏启动步骤">
    {tasks.map(task => <StartupRow key={task.id} task={task} />)}
  </ol></div>;
}

function StartupRow({task}: {task: RuntimeStartupTaskV1}) {
  const completed = task.state === "COMPLETED";
  const failed = task.state === "FAILED";
  const name = startupTaskNames[task.kind];
  const percentage = completed ? 100 : startupPercentage(task);
  const status = completed ? "已完成" : failed ? "失败" : "进行中";
  return <li className={`player-startup-row${completed ? " is-completed" : ""}${failed ? " is-failed" : ""}`}
    data-task-id={task.id} data-task-kind={task.kind} data-task-state={task.state} data-task-summary={task.summary ? "true" : undefined}>
    <StartupLabel name={name} />
    <StartupProgress name={name} percentage={percentage} running={task.state === "RUNNING"} failed={failed} />
    <span className="player-startup-indicator" role="img" aria-label={`${name}${status}`}>
      {completed ? <svg viewBox="0 0 24 24" aria-hidden="true" className="player-startup-check"><path d="m5 12 4 4 10-10" /></svg>
        : failed ? <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 6 12 12M18 6 6 18" /></svg>
          : <svg viewBox="0 0 24 24" aria-hidden="true" className="player-startup-spinner"><circle cx="12" cy="12" r="9" /></svg>}
    </span>
  </li>;
}

function StartupProgress({name, percentage, running, failed}: {name: string; percentage: number | null; running: boolean; failed: boolean}) {
  const filled = Math.floor((percentage ?? 0) / 10);
  const unknown = percentage === null && running;
  return <span className={`player-startup-progress${unknown ? " is-indeterminate" : ""}`}
    role="progressbar" aria-label={`${name}进度`} aria-valuemin={0} aria-valuemax={100}
    aria-valuenow={percentage ?? undefined} aria-valuetext={failed ? "加载失败" : unknown ? "进度未知，正在处理" : undefined}>
    <span aria-hidden="true">[</span>
    {Array.from({length: 10}, (_, index) => <span key={index} aria-hidden="true"
      className={`player-startup-cell${index < filled ? " is-filled" : ""}`}
      style={{"--startup-cell": index} as CSSProperties}>{index < filled ? "▪" : "."}</span>)}
    <span aria-hidden="true">]</span>
  </span>;
}

function StartupLabel({name}: {name: string}) {
  const ref = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => {
    const container = ref.current;
    const text = container?.firstElementChild;
    if (!container || !(text instanceof HTMLElement)) {return;}
    const measure = () => {
      const overflow = Math.max(0, text.scrollWidth - container.clientWidth);
      container.style.setProperty("--startup-label-travel", `${-overflow}px`);
      container.dataset.overflow = overflow > 1 ? "true" : "false";
    };
    measure();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure);
    observer?.observe(container);
    observer?.observe(text);
    return () => observer?.disconnect();
  }, [name]);
  return <span ref={ref} className="player-startup-label" title={name}><span>{name}</span></span>;
}
