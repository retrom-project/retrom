"use client";

import Image from "next/image";
import Link from "next/link";
import { FormEvent, useState } from "react";
import { useRouter } from "next/navigation";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { AppIcon } from "@/components/app-icon";
import { FeedbackBanner, StatusBadge } from "@/components/ui";
import { LaunchButton } from "@/features/player/launch-button";
import { writeHeaders } from "@/lib/api/client";
import { formatTime } from "@/lib/backend";
import { responseError } from "@/lib/upload";

export type SaveItem = {
  saveStateId: string;
  gameId: string;
  gameTitle: string;
  name: string;
  version: number;
  createdAtMs: number;
  activeDurationMs: number;
  screenshotUrl: string;
  core?: { id: string; name: string };
  availability?: { status: "AVAILABLE" | "BLOCKED"; reasons: Array<{ code?: string; logicalName?: string }> };
};

export function SaveManager({ saves }: { saves: SaveItem[] }) {
  const router = useRouter();
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [pendingDelete, setPendingDelete] = useState<SaveItem | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);

  async function rename(event: FormEvent<HTMLFormElement>, save: SaveItem) {
    event.preventDefault(); setBusy(save.saveStateId); setError(""); setNotice("");
    const data = new FormData(event.currentTarget);
    try {
      const response = await fetch(`/api/v1/saves/${save.saveStateId}`, { method: "PATCH", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${save.version}"` }), body: JSON.stringify({ name: String(data.get("name") ?? "") }) });
      if (!response.ok) throw new Error(await responseError(response, "存档重命名失败"));
      setNotice(`存档“${save.name}”已创建新的名称版本。`); setEditingId(null); router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "存档重命名失败"); }
    finally { setBusy(null); }
  }

  async function remove(save: SaveItem) {
    setBusy(save.saveStateId); setError(""); setNotice("");
    try {
      const response = await fetch(`/api/v1/saves/${save.saveStateId}`, { method: "DELETE", credentials: "same-origin", headers: await writeHeaders({ "If-Match": `"v${save.version}"` }) });
      if (!response.ok) throw new Error(await responseError(response, "存档删除失败"));
      setNotice(`存档“${save.name}”已删除，底层内容会继续按保留期保护。`); router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "存档删除失败"); }
    finally { setBusy(null); setPendingDelete(null); }
  }

  return <div className="stack">
    {notice ? <FeedbackBanner tone="good">{notice}</FeedbackBanner> : null}
    {error ? <FeedbackBanner tone="bad">{error}</FeedbackBanner> : null}
    <div className="save-card-grid">{saves.map((save) => {
      const available = save.availability?.status !== "BLOCKED";
      const duration = Math.max(0, Math.round(save.activeDurationMs / 60_000));
      return <article className="save-card" key={save.saveStateId}>
        <div className="save-shot">
          <Image src={save.screenshotUrl} alt={`${save.gameTitle} 存档画面`} width={640} height={360} unoptimized />
          <span className="save-shot-status"><StatusBadge tone={available ? "good" : "bad"}>{available ? "可以继续" : "当前不可用"}</StatusBadge></span>
          <button className="save-delete" type="button" aria-label={`删除存档“${save.name}”`} title="删除存档" disabled={busy !== null} onClick={() => setPendingDelete(save)}><AppIcon name="x" /></button>
          <div className="save-shot-action">{available ? <LaunchButton gameId={save.gameId} saveStateId={save.saveStateId} returnTo="/saves" label="从这里继续" /> : <button className="button" disabled>当前不可继续</button>}</div>
        </div>
        <div className="save-card-body">
          {editingId === save.saveStateId ? <form className="save-inline-editor" onSubmit={(event) => void rename(event, save)}><label className="sr-only" htmlFor={`save-name-${save.saveStateId}`}>存档名称</label><input id={`save-name-${save.saveStateId}`} name="name" defaultValue={save.name} required maxLength={120} autoFocus /><button className="button compact" disabled={busy !== null}>保存</button><button className="button secondary compact" type="button" disabled={busy !== null} onClick={() => setEditingId(null)}>取消</button></form> : <div className="save-title-row"><h2>{save.name}</h2><button className="icon-button" type="button" aria-label={`编辑存档“${save.name}”的名称`} title="修改存档名称" onClick={() => setEditingId(save.saveStateId)}><AppIcon name="pencil" /></button></div>}
          <Link className="save-game-title" href={`/games/${save.gameId}`}>{save.gameTitle}</Link>
          <div className="save-facts"><span>{save.core?.name ?? "运行核心未知"}</span><time dateTime={new Date(save.createdAtMs).toISOString()}>{formatTime(save.createdAtMs)}</time><strong>已游玩 {duration} 分钟</strong></div>
          {!available ? <p role="alert">{save.availability?.reasons.map((reason) => reason.logicalName ? `${reason.logicalName} 当前不可用` : "运行依赖当前不可用").join("；") || "游戏或运行依赖当前不可用。"}</p> : null}
        </div>
      </article>;
    })}</div>
    <ConfirmDialog open={pendingDelete !== null} title="删除这份存档？" description={`“${pendingDelete?.name ?? ""}”将从你的存档列表中移除。`} confirmLabel="删除存档" tone="danger" busy={busy !== null} onCancel={() => setPendingDelete(null)} onConfirm={() => { if (pendingDelete) void remove(pendingDelete); }}><ul><li>删除后不能再从这份进度继续</li><li>底层内容会先进入引用保护期，不会立即清除</li></ul></ConfirmDialog>
  </div>;
}
