"use client";

import Image from "next/image";
import Link from "next/link";
import { FormEvent, useState } from "react";
import { useRouter } from "next/navigation";
import { StatusBadge } from "@/components/ui";
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

  async function rename(event: FormEvent<HTMLFormElement>, save: SaveItem) {
    event.preventDefault(); setBusy(save.saveStateId); setError(""); setNotice("");
    const data = new FormData(event.currentTarget);
    try {
      const response = await fetch(`/api/v1/saves/${save.saveStateId}`, { method: "PATCH", credentials: "same-origin", headers: await writeHeaders({ "Content-Type": "application/json", "If-Match": `"v${save.version}"` }), body: JSON.stringify({ name: String(data.get("name") ?? "") }) });
      if (!response.ok) throw new Error(await responseError(response, "存档重命名失败"));
      setNotice(`存档“${save.name}”已创建新的名称版本。`); router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "存档重命名失败"); }
    finally { setBusy(null); }
  }

  async function remove(save: SaveItem) {
    if (!window.confirm(`删除存档“${save.name}”？删除后会先进入引用保护期。`)) return;
    setBusy(save.saveStateId); setError(""); setNotice("");
    try {
      const response = await fetch(`/api/v1/saves/${save.saveStateId}`, { method: "DELETE", credentials: "same-origin", headers: await writeHeaders({ "If-Match": `"v${save.version}"` }) });
      if (!response.ok) throw new Error(await responseError(response, "存档删除失败"));
      setNotice(`存档“${save.name}”已删除，底层内容会继续按保留期保护。`); router.refresh();
    } catch (caught) { setError(caught instanceof Error ? caught.message : "存档删除失败"); }
    finally { setBusy(null); }
  }

  return <div className="stack">
    {notice ? <p role="status" className="status good">{notice}</p> : null}
    {error ? <p role="alert" className="status bad">{error}</p> : null}
    <div className="save-card-grid">{saves.map((save) => {
      const available = save.availability?.status !== "BLOCKED";
      const duration = Math.max(0, Math.round(save.activeDurationMs / 60_000));
      return <article className="save-card" key={save.saveStateId}><Image src={save.screenshotUrl} alt={`${save.name} 存档画面`} width={640} height={360} unoptimized /><div className="save-card-body"><StatusBadge tone={available ? "good" : "bad"}>{available ? "可以继续" : "当前不可用"}</StatusBadge><h2>{save.name}</h2><p><Link className="row-action" href={`/games/${save.gameId}`}>{save.gameTitle}</Link><br />{save.core?.name ? `${save.core.name} · ` : ""}{formatTime(save.createdAtMs)}<br />已游玩 {duration} 分钟</p>{!available ? <p role="alert">{save.availability?.reasons.map((reason) => reason.logicalName ? `${reason.logicalName} 当前不可用` : "运行依赖当前不可用").join("；") || "游戏或运行依赖当前不可用。"}</p> : null}<div className="save-card-actions">{available ? <LaunchButton gameId={save.gameId} saveStateId={save.saveStateId} returnTo="/saves" label="从这里继续" /> : <button className="button" disabled>从这里继续</button>}<details className="save-actions"><summary className="button secondary">管理存档</summary><form className="save-editor" onSubmit={(event) => void rename(event, save)}><label>存档名称<input name="name" defaultValue={save.name} required maxLength={120} /></label><div className="header-actions"><button className="button secondary" disabled={busy !== null}>保存名称</button><button className="button danger" type="button" disabled={busy !== null} onClick={() => void remove(save)}>删除存档</button></div></form></details></div></div></article>;
    })}</div>
  </div>;
}
