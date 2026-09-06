"use client";

import {useCallback, useEffect, useState} from "react";
import {useAuth} from "@/features/auth/auth-provider";
import {ConfirmDialog} from "@/components/confirm-dialog";
import {deleteGameSaveDraft, listGameSaveDrafts, type GameSaveDraft} from "./game-save-draft-store";
import {draftIsActive} from "./game-save-draft-lease";
import {uploadLocalGameSave} from "./local-game-save-upload";

export function LocalGameSaveNotice({pathname}: {pathname: string}) {
  const {context} = useAuth();
  const userId = context.user?.userId;
  const [drafts, setDrafts] = useState<GameSaveDraft[]>([]);
  const [selected, setSelected] = useState<GameSaveDraft | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const visible = Boolean(userId) && !pathname.includes("/play/") && !pathname.includes("review-previews") &&
    !["/login", "/setup", "/register"].includes(pathname);
  const refresh = useCallback(async () => {
    if (!userId) {return;}
    try {setDrafts((await listGameSaveDrafts(userId)).filter((draft) => !draftIsActive(userId, draft.launchId)));}
    catch { /* No banner when the browser cannot read its draft database. */ }
  }, [userId]);
  useEffect(() => {
    if (!visible) {return;}
    let active = true;
    const update = () => {if (active) {void refresh();}};
    update(); const timer = window.setInterval(update, 15_000);
    return () => {active = false; window.clearInterval(timer);};
  }, [pathname, refresh, visible]);
  const complete = async (save: boolean) => {
    if (!selected || busy || selected.userId !== userId) {return;}
    if (draftIsActive(selected.userId, selected.launchId)) {setError("这份草稿正在其他游戏页面中使用，请先退出该页面。"); return;}
    setBusy(true); setError("");
    try {
      if (save) {await uploadLocalGameSave(selected);}
      await deleteGameSaveDraft(selected.userId, selected.launchId, selected.payload.requestId);
      setSelected(null); await refresh();
    } catch (failure) {setError(failure instanceof Error ? failure.message : "操作失败，草稿已保留。");}
    finally {setBusy(false);}
  };
  const ownDrafts = drafts.filter((draft) => draft.userId === userId);
  if (!visible || !ownDrafts.length) {return null;}
  return <aside className="local-game-save-notice" aria-label="未提交的本地游戏存档">
    <p>此浏览器有 {ownDrafts.length} 份未提交的游戏存档草稿。它们不会自动载入新游戏。</p>
    <button className="button secondary" type="button" onClick={() => {setSelected(ownDrafts[0]); setError("");}}>处理本地草稿</button>
    <ConfirmDialog open={selected?.userId === userId} title="处理未提交的游戏存档" busy={busy}
      description={selected ? `${selected.title} · ${new Date(selected.updatedAtMs).toLocaleString("zh-CN")}。${selected.restored ? "保存将更新当时选择的存档。" : "保存将创建独立存档。"}平台只提交游戏已写入的数据，不是关闭页面时的即时进度。` : ""}
      leadingLabel="保存到服务端" confirmLabel="丢弃草稿" cancelLabel="稍后处理" tone="danger" portalToBody
      onLeading={() => {void complete(true);}} onConfirm={() => {void complete(false);}} onCancel={() => setSelected(null)}>
      {error ? <p role="alert">{error}</p> : null}
    </ConfirmDialog>
  </aside>;
}
