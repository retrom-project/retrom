import Link from "next/link";

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
  const cacheFailure = state === "error" && /^CONTENT_IO_(?:CACHE_UNAVAILABLE|WORKSPACE_UNAVAILABLE)$/u.test(message);
  const downloadFailure = state === "error" && /^CONTENT_IO_(?:NETWORK_FAILED|TIMEOUT|PRELOAD_FAILED)$/u.test(message);
  const retryable = cacheFailure || downloadFailure;
  const startupFailure = startupErrorMessage(state, message);
  const visibleTasks = startupTaskViews(state, tasks, progress);
  return <div className="player-loading" role="status" aria-live="polite">
    <strong>{state === "loading" ? "游戏启动中" : "游戏启动失败"}</strong>
    {state === "loading" && visibleTasks.length === 0 ? <i aria-hidden="true" /> : null}
    {state === "error" ? <LoadingError message={startupFailure ?? message} cacheFailure={cacheFailure} downloadFailure={downloadFailure} /> : null}
    <RetryActions retryable={retryable} startupFailure={startupFailure} canLoadOnDemand={canLoadOnDemand} />
    {visibleTasks.length > 0 ? <PlayerStartupTasks tasks={visibleTasks} /> : null}
    <LoadingNote state={state} retryable={retryable || startupFailure !== undefined} returnTo={returnTo} immersive={immersive} tasks={tasks} progress={progress} />
  </div>;
}

function startupTaskViews(state: "loading" | "error", tasks: RuntimeStartupTaskV1[], progress: PlayerLoadProgress | null): RuntimeStartupTaskV1[] {
  if (tasks.length) {
    return state === "error" ? tasks.map(task => task.state === "RUNNING" ? {...task, state: "FAILED" as const} : task) : tasks;
  }
  return progress && progressPercentage(progress) !== null
    ? [{id: "host:content-progress", kind: "GAME_CONTENT", state: state === "error" ? "FAILED" : "RUNNING", progress}] : [];
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

function LoadingError({message, cacheFailure, downloadFailure}: {message: string; cacheFailure: boolean; downloadFailure: boolean}) {
  return <p className="player-loading-error">{cacheFailure ? "无法完成本地缓存，请检查浏览器存储权限和可用空间。"
    : downloadFailure ? "内容下载未完成，请检查网络后重试。" : message}</p>;
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
