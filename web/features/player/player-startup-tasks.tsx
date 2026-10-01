import type {RuntimeStartupTaskV1} from "./runtime/contract";
import type {CSSProperties} from "react";
import {startupLabels, startupPercentage} from "./startup-task-model";

export function PlayerStartupTasks({tasks}: {tasks: RuntimeStartupTaskV1[]}) {
  return <ol className="player-startup-tasks" aria-label="游戏启动步骤">
    {tasks.map((task, index) => <StartupRow key={task.id} task={task} position={3 - tasks.length + index} />)}
  </ol>;
}

function StartupRow({task, position}: {task: RuntimeStartupTaskV1; position: number}) {
  const completed = task.state === "COMPLETED";
  const failed = task.state === "FAILED";
  const label = startupLabels[task.kind][completed ? 1 : 0];
  const percentage = startupPercentage(task);
  return <li className={`player-startup-row${completed ? " is-completed" : ""}${failed ? " is-failed" : ""}`}
    style={{"--startup-row": position} as CSSProperties}
    data-task-id={task.id} data-task-kind={task.kind} data-task-state={task.state}>
    <span className="player-startup-indicator">
      {completed ? <svg viewBox="0 0 24 24" aria-hidden="true" className="player-startup-check"><path d="m5 12 4 4 10-10" /></svg>
        : failed ? <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 6 12 12M18 6 6 18" /></svg>
          : percentage === null ? <svg viewBox="0 0 24 24" aria-hidden="true" className="player-startup-spinner"><circle cx="12" cy="12" r="9" /></svg>
            : <span role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={percentage}>{percentage}%</span>}
    </span>
    <span>{failed ? `${label.replace(/中$/u, "")}失败` : label}</span>
  </li>;
}
