import Link from "next/link";

import { formatPlayerBytes } from "./player-shell-model";
import { ContentPreloadRetry } from "./content-preload-retry";
import { PlayerStartupRetry } from "./player-startup-retry";
import { startupFailureMessage } from "./startup-failure";
import {PlayerStartupTasks} from "./player-startup-tasks";
import type {RuntimeStartupTaskV1} from "./runtime/contract";

export type PlayerLoadProgress = {
  loadedBytes: number;
  totalBytes: number;
};

type PlayerLoadingProps = {
  tasks?: RuntimeStartupTaskV1[];
  immersive: boolean;
  canLoadOnDemand?: boolean;
  message: string;
  progress: PlayerLoadProgress | null;
  returnTo: string;
  state: "loading" | "error";
};

export function PlayerLoading({ state, message, progress, returnTo, immersive, canLoadOnDemand, tasks = [] }: PlayerLoadingProps) {
  return message === "RUNTIME_SESSION_UNAVAILABLE" ? <UnavailableSession returnTo={returnTo} immersive={immersive} /> :
    <StartupLoading state={state} message={message} progress={progress} returnTo={returnTo} immersive={immersive} canLoadOnDemand={canLoadOnDemand} tasks={tasks} />;
}

function UnavailableSession({returnTo, immersive}: Pick<PlayerLoadingProps, "returnTo" | "immersive">) {
  return <div className="player-loading" role="alert">
    <strong>运行会话已不可用</strong>
    <p>服务器已拒绝继续此会话，游戏已停止。游戏可能已被删除，或运行凭据已失效。</p>
    <Link href={returnTo}>{immersive ? "返回游戏列表" : "返回游戏库"}</Link>
  </div>;
}

function StartupLoading({state, message, progress, returnTo, immersive, canLoadOnDemand, tasks = []}: PlayerLoadingProps) {
  const percentage = progressPercentage(progress);
  const cacheFailure = state === "error" && /^CONTENT_IO_(?:CACHE_UNAVAILABLE|WORKSPACE_UNAVAILABLE)$/u.test(message);
  const downloadFailure = state === "error" && /^CONTENT_IO_(?:NETWORK_FAILED|TIMEOUT|PRELOAD_FAILED)$/u.test(message);
  const retryable = cacheFailure || downloadFailure;
  const startupFailure = startupErrorMessage(state, message);
  return <div className="player-loading" role="status" aria-live="polite">
    {state === "loading" && tasks.length === 0 ? <i aria-hidden="true" /> : null}
    <LoadingTitle state={state} tasks={tasks} message={startupFailure ?? message} cacheFailure={cacheFailure} downloadFailure={downloadFailure} />
    <RetryActions retryable={retryable} startupFailure={startupFailure} canLoadOnDemand={canLoadOnDemand} />
    {tasks.length > 0 ? <PlayerStartupTasks tasks={startupTaskViews(state, tasks)} /> : null}
    {state === "loading" && tasks.length === 0 && progress && percentage !== null ? <div className="player-loading-progress">
      <div
        className="player-loading-progress-track"
        role="progressbar"
        aria-label="游戏内容加载进度"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percentage}
      ><span style={{ width: `${percentage}%` }} /></div>
      <small>{formatPlayerBytes(progress.loadedBytes)} / {formatPlayerBytes(progress.totalBytes)} · {percentage}%</small>
    </div> : null}
    <LoadingNote state={state} retryable={retryable || startupFailure !== undefined} returnTo={returnTo} immersive={immersive} tasks={tasks} progress={progress} />
  </div>;
}

function startupTaskViews(state: "loading" | "error", tasks: RuntimeStartupTaskV1[]) {
  return state === "error" ? tasks.map(task => task.state === "RUNNING" ? {...task, state: "FAILED" as const} : task) : tasks;
}

function startupErrorMessage(state: PlayerLoadingProps["state"], message: string) {
  return state === "error" ? startupFailureMessage(message) : undefined;
}

function RetryActions({retryable, startupFailure, canLoadOnDemand}: {
  retryable: boolean; startupFailure: string | undefined; canLoadOnDemand: boolean | undefined;
}) {
  if (retryable) {return <ContentPreloadRetry canLoadOnDemand={canLoadOnDemand === true} />;}
  return startupFailure ? <PlayerStartupRetry /> : null;
}

function LoadingTitle({state, tasks, message, cacheFailure, downloadFailure}: {
  state: PlayerLoadingProps["state"]; tasks: RuntimeStartupTaskV1[]; message: string; cacheFailure: boolean; downloadFailure: boolean;
}) {
  return <strong>{state === "loading" && tasks.length > 0 && tasks.every(task => task.state !== "RUNNING")
    ? <svg className="player-startup-spinner player-startup-pending" viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="9" /></svg> : null}
    {cacheFailure ? "无法完成本地缓存，请检查浏览器存储权限和可用空间。"
      : downloadFailure ? "内容下载未完成，请检查网络后重试。" : message}</strong>;
}

function LoadingNote({state, retryable, returnTo, immersive, tasks, progress}: Pick<PlayerLoadingProps, "state" | "returnTo" | "immersive" | "progress"> & {retryable: boolean; tasks: RuntimeStartupTaskV1[]}) {
  return <p>{state === "error"
    ? <><span>{retryable ? "已下载的有效内容会在重试时复用。" : "凭据可能已过期或依赖不兼容。"}</span> <Link href={returnTo}>{immersive ? "返回游戏列表" : "返回游戏库"}</Link></>
    : tasks.length > 0 ? "正在准备本次启动所需内容，已缓存的资源会直接复用。"
      : progress ? "首次加载会写入本地缓存；再次启动相同版本时将直接复用。"
        : "页面会在验证和指定存档恢复后自动开始，无需再次点击。"}</p>;
}

function progressPercentage(progress: PlayerLoadProgress | null) {
  if (!progress || !Number.isSafeInteger(progress.loadedBytes) || !Number.isSafeInteger(progress.totalBytes) ||
    progress.loadedBytes < 0 || progress.totalBytes <= 0 || progress.loadedBytes > progress.totalBytes) {return null;}
  return Math.min(100, Math.floor(progress.loadedBytes * 100 / progress.totalBytes));
}
