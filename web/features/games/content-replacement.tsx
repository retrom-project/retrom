"use client";

import { useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import type { Schema } from "@/lib/api/types";
import { useContentReplacement } from "./use-content-replacement";

const phases: Record<Schema<"ContentReplacement">["phase"], string> = {
  queued: "内容替换正在等待",
  preparing: "正在准备游戏内容",
  normalizing: "正在转换游戏内容",
  configuring: "正在检查游戏内容",
  packaging: "正在打包并校验游戏内容",
  publishing: "正在发布替换结果",
  completed: "游戏内容已替换",
};
const terminal: Record<string, string> = {
  canceling: "正在取消内容替换",
  cancelled: "内容替换已取消",
  failed: "内容替换失败",
  interrupted: "内容替换已中断",
};

export function GameContentReplacement({ detail, onChange }: {
  detail: Schema<"GameDetail">; onChange: () => void;
}) {
  const replacement = useContentReplacement(detail.game.id, detail.game.version, onChange);
  const [open, setOpen] = useState(false);
  const [path, setPath] = useState("/");
  const operation = replacement.operation;
  async function submit() {
    if (await replacement.start(path, detail.game.version)) { setOpen(false); }
  }
  return (
    <article>
      <div>
        <h3>替换游戏内容</h3>
        <p>从服务器目录选择新内容，准备完成后生效。现有存档按实际内容身份判断能否恢复。</p>
        {operation && !replacement.expired ? (
          <div className="stack" role="status" aria-live="polite">
            <strong>{terminal[operation.status] ?? phases[operation.phase]}</strong>
            {operation.error ? <p>{operation.error.message}</p> : null}
            {operation.canCancel ? (
              <button className="button secondary" disabled={replacement.busy} onClick={() => void replacement.cancel()}>取消内容替换</button>
            ) : null}
            {operation.phase === "publishing" && operation.status === "running" ? <p>结果确认期间无法取消。</p> : null}
          </div>
        ) : null}
        {replacement.readError ? (
          <div className="stack" role="alert">
            <p>{replacement.readError}</p>
            <button className="button secondary" disabled={replacement.busy} onClick={replacement.refresh}>重试读取进度</button>
          </div>
        ) : null}
        {(operation && ["failed", "interrupted"].includes(operation.status)) || replacement.expired ? (
          <button className="button secondary" onClick={onChange}>刷新游戏资料</button>
        ) : null}
      </div>
      <button className="button secondary" disabled={!replacement.checked || replacement.active || replacement.busy} onClick={() => setOpen(true)}>
        替换游戏内容
      </button>
      <ConfirmDialog open={open} title="替换游戏内容" description="后台准备完成后替换生效，现有存档可能无法恢复。可以关闭页面，稍后在此查看进度。"
        busy={replacement.busy} onCancel={() => setOpen(false)} onConfirm={() => void submit()} confirmDisabled={!path.startsWith("/")}>
        <div className="stack">
          <label className="field">服务器内容路径
            <input aria-label="服务器内容路径" placeholder="/path/to/game.zip" value={path} onChange={(event) => setPath(event.target.value)} />
          </label>
          <p className="workspace-note">填写服务进程可见的绝对路径。来源文件保持不变。</p>
        </div>
      </ConfirmDialog>
    </article>
  );
}
