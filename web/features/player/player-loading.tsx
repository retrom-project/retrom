import {PlayerFailure} from "./player-failure";
import Link from "next/link";
import {playerReturnLabel, type PlayerReturnIntent} from "@/lib/navigation/player-return";

import { ContentPreloadRetry } from "./content-preload-retry";
import { PlayerStartupRetry } from "./player-startup-retry";
import { startupFailure } from "./startup-failure";
import {PlayerStartupTasks} from "./player-startup-tasks";
import type {RuntimeStartupTaskV1, RuntimeFailureV1} from "./runtime/contract";

export type PlayerLoadProgress = {
  loadedBytes: number;
  totalBytes: number;
};

type PlayerLoadingProps = {
  failure?: RuntimeFailureV1 | null;
  onRetry: () => void;
  tasks?: RuntimeStartupTaskV1[];
  canLoadOnDemand?: boolean;
  message: string;
  progress: PlayerLoadProgress | null;
  returnIntent: PlayerReturnIntent;
  state: "loading" | "error";
};

export function PlayerLoading({ failure, onRetry, state, message, progress, returnIntent, canLoadOnDemand, tasks = [] }: PlayerLoadingProps) {
  if (state === "error" && failure) {return <PlayerFailure failure={failure} onRetry={onRetry} returnIntent={returnIntent} />;}
  return message === "RUNTIME_SESSION_UNAVAILABLE" ? <UnavailableSession returnIntent={returnIntent} /> :
    <StartupLoading onRetry={onRetry} state={state} message={message} progress={progress} returnIntent={returnIntent} canLoadOnDemand={canLoadOnDemand} tasks={tasks} />;
}

function UnavailableSession({returnIntent}: Pick<PlayerLoadingProps, "returnIntent">) {
  return <div className="player-loading" role="alert">
    <strong>运行会话已不可用</strong>
    <p>服务器已拒绝继续此会话，游戏已停止。游戏可能已被删除，或运行凭据已失效。</p>
    <Link href={returnIntent.href}>{playerReturnLabel(returnIntent)}</Link>
  </div>;
}

function StartupLoading({onRetry, state, message, progress, returnIntent, canLoadOnDemand, tasks = []}: PlayerLoadingProps) {
  const cacheFailure = state === "error" && /^CONTENT_IO_(?:CACHE_UNAVAILABLE|WORKSPACE_UNAVAILABLE)$/u.test(message);
  const downloadFailure = state === "error" && /^CONTENT_IO_(?:NETWORK_FAILED|TIMEOUT|PRELOAD_FAILED)$/u.test(message);
  const retryable = cacheFailure || downloadFailure;
  const failure = startupFailure(message);
  const visibleTasks = startupTaskViews(state, tasks, progress);
  return <div className="player-loading" role="status" aria-live="polite">
    <strong>{state === "loading" ? "游戏启动中" : "游戏启动失败"}</strong>
    {state === "loading" && visibleTasks.length === 0 ? <i aria-hidden="true" /> : null}
    {state === "error" ? <LoadingError message={failure.message} cacheFailure={cacheFailure} downloadFailure={downloadFailure} /> : null}
    {state === "error" ? <code>{/^[A-Z][A-Z0-9_]{1,127}$/u.test(message) ? message : "PLAYER_RUNTIME_FAILED"}</code> : null}
    <RetryActions onRetry={onRetry} retryable={retryable} startupRetryable={state === "error" && failure.retryable} canLoadOnDemand={canLoadOnDemand} />
    {visibleTasks.length > 0 ? <PlayerStartupTasks tasks={visibleTasks} /> : null}
    <LoadingNote state={state} retryable={retryable || state === "error" && failure.retryable} returnIntent={returnIntent} tasks={tasks} progress={progress} />
  </div>;
}

function startupTaskViews(state: "loading" | "error", tasks: RuntimeStartupTaskV1[], progress: PlayerLoadProgress | null): RuntimeStartupTaskV1[] {
  if (tasks.length) {
    return state === "error" ? tasks.map(task => task.state === "RUNNING" ? {...task, state: "FAILED" as const} : task) : tasks;
  }
  return progress && progressPercentage(progress) !== null
    ? [{id: "host:content-progress", kind: "GAME_CONTENT", state: state === "error" ? "FAILED" : "RUNNING", progress}] : [];
}

function RetryActions({retryable, startupRetryable, canLoadOnDemand, onRetry}: {
  retryable: boolean; startupRetryable: boolean; canLoadOnDemand: boolean | undefined; onRetry: () => void;
}) {
  if (retryable) {return <ContentPreloadRetry canLoadOnDemand={canLoadOnDemand === true} />;}
  return startupRetryable ? <PlayerStartupRetry onRetry={onRetry} /> : null;
}

function LoadingError({message, cacheFailure, downloadFailure}: {message: string; cacheFailure: boolean; downloadFailure: boolean}) {
  return <p className="player-loading-error">{cacheFailure ? "无法完成本地缓存，请检查浏览器存储权限和可用空间。"
    : downloadFailure ? "内容下载未完成，请检查网络后重试。" : message}</p>;
}

function LoadingNote({state, retryable, returnIntent, tasks, progress}: Pick<PlayerLoadingProps, "state" | "returnIntent" | "progress"> & {retryable: boolean; tasks: RuntimeStartupTaskV1[]}) {
  return <p>{state === "error"
    ? <><span>{retryable ? "已下载的有效内容会在重试时复用。" : "请根据提示处理后再启动。"}</span> <Link href={returnIntent.href}>{playerReturnLabel(returnIntent)}</Link></>
    : tasks.length > 0 ? "正在准备本次启动所需内容，已缓存的资源会直接复用。"
      : progress ? "首次加载会写入本地缓存；再次启动相同版本时将直接复用。"
        : "页面会在验证和指定存档恢复后自动开始，无需再次点击。"}</p>;
}

function progressPercentage(progress: PlayerLoadProgress | null) {
  if (!progress || !Number.isSafeInteger(progress.loadedBytes) || !Number.isSafeInteger(progress.totalBytes) ||
    progress.loadedBytes < 0 || progress.totalBytes <= 0 || progress.loadedBytes > progress.totalBytes) {return null;}
  return Math.min(100, Math.floor(progress.loadedBytes * 100 / progress.totalBytes));
}
