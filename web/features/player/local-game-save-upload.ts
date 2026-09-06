import {writeHeaders, handleAuthenticationResponse} from "@/lib/api/client";
import type {GameSaveDraft} from "./game-save-draft-store";
import {createSaveForm} from "./player-session";
import {prepareManualSaveScreenshot} from "./manual-save-screenshot";

export async function uploadLocalGameSave(draft: GameSaveDraft) {
  const image = await prepareManualSaveScreenshot({screenshot: draft.payload.screenshot,
    format: draft.payload.screenshot.type === "image/jpeg" ? "jpeg" : "png"});
  if (!image || !draft.payload.requestId) {throw Error("草稿截图不完整，已保留本地数据。");}
  const body = createSaveForm(draft.payload, image, undefined);
  const response = handleAuthenticationResponse(await fetch(`/api/v1/launches/${draft.launchId}/local-save`, {
    method: "POST", credentials: "same-origin", headers: writeHeaders({"Idempotency-Key": draft.payload.requestId}), body,
  }));
  if (response.status === 409) {throw Error("原存档已更新或删除，无法覆盖。此浏览器中的草稿仍保留。");}
  if (!response.ok) {throw Error("保存失败，本地草稿已保留，请稍后重试。");}
}
