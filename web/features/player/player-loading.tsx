import Link from "next/link";

import { formatPlayerBytes } from "./player-shell-model";

export type PlayerLoadProgress = {
  loadedBytes: number;
  totalBytes: number;
};

type PlayerLoadingProps = {
  immersive: boolean;
  message: string;
  progress: PlayerLoadProgress | null;
  returnTo: string;
  state: "loading" | "error";
};

export function PlayerLoading({ state, message, progress, returnTo, immersive }: PlayerLoadingProps) {
  const percentage = progressPercentage(progress);
  return <div className="player-loading" role="status" aria-live="polite">
    {state === "loading" ? <i aria-hidden="true" /> : null}
    <strong>{message}</strong>
    {state === "loading" && progress && percentage !== null ? <div className="player-loading-progress">
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
    <p>{state === "error"
      ? <><span>凭据可能已过期或依赖不兼容。</span> <Link href={returnTo}>{immersive ? "返回游戏列表" : "返回游戏库"}</Link></>
      : progress
        ? "首次加载会写入本地缓存；再次启动相同版本时将直接复用。"
        : "页面会在验证和指定存档恢复后自动开始，无需再次点击。"}</p>
  </div>;
}

function progressPercentage(progress: PlayerLoadProgress | null) {
  if (!progress || !Number.isSafeInteger(progress.loadedBytes) || !Number.isSafeInteger(progress.totalBytes) ||
    progress.loadedBytes < 0 || progress.totalBytes <= 0 || progress.loadedBytes > progress.totalBytes) {return null;}
  return Math.min(100, Math.floor(progress.loadedBytes * 100 / progress.totalBytes));
}
