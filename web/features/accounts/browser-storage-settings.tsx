"use client";
import { useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { formatBytes } from "@/lib/format-bytes";
import { useBrowserStorage } from "./use-browser-storage";

export function BrowserStorageSettings() {
  const storage = useBrowserStorage();
  const [confirm, setConfirm] = useState(false);
  const unavailable = storage.busy === "refresh" ? "正在读取…" : "暂不可用";
  return <section className="panel browser-storage-settings" aria-labelledby="browser-storage-heading">
    <div className="panel-head">
      <div><h2 id="browser-storage-heading">本浏览器存储</h2><p>查看当前站点在此浏览器中的占用，清理可重新下载的游戏资源。</p></div>
      <button className="button secondary" disabled={storage.busy !== null} onClick={() => void storage.refresh()}>
        {storage.busy === "refresh" ? "正在刷新…" : "刷新"}
      </button>
    </div>
    <div className="panel-body stack">
      <dl className="browser-storage-facts">
        <div><dt>站点本地占用（估算）</dt><dd>{storage.usage === null ? unavailable : `约 ${formatBytes(storage.usage)}`}</dd></div>
        <div><dt>资源缓存有效数据</dt><dd>{storage.cache === null ? unavailable : `${formatBytes(storage.cache.cachedBytes)} · ${storage.cache.objectCount} 项`}</dd></div>
      </dl>
      <p className="muted">浏览器占用估算与缓存有效数据的统计口径不同。仅统计当前站点，不包含游戏子域自行保存的数据。</p>
      {storage.usageError ? <p role="status">{storage.usageError}</p> : null}
      {storage.cacheError ? <p role="status">{storage.cacheError}</p> : null}
      {storage.cache && !storage.cache.canClear ? <p>当前浏览器不支持安全清理资源缓存。</p> : null}
      <div className="workspace-actions">
        <button className="button secondary" disabled={storage.busy !== null || !storage.cache?.canClear || storage.cache.objectCount === 0}
          onClick={() => setConfirm(true)}>清理资源缓存</button>
        {storage.busy === "clear" ? <button className="button secondary" onClick={storage.stop}>停止清理</button> : null}
      </div>
      <p className="muted">保留存档、未同步进度和登录状态；正在使用的资源会跳过。清理后再次游玩可能需要重新下载。</p>
      {storage.message ? <p role="status" aria-live="polite">{storage.message}</p> : null}
    </div>
    <ConfirmDialog open={confirm} title="清理资源缓存？" confirmLabel="清理缓存" portalToBody
      description="将清理此浏览器中闲置的游戏内容和运行资源缓存。存档与未同步进度会保留，再次游玩可能需要重新下载。"
      onCancel={() => setConfirm(false)} onConfirm={() => { setConfirm(false); void storage.clear(); }} />
  </section>;
}
