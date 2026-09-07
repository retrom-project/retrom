import type {RuntimeCheckpointAvailabilityV1} from "./runtime/contract";

export type CheckpointSemantics = "INSTANT" | "GAME_SAVE";
export type NativeSaveCapabilities = RuntimeCheckpointAvailabilityV1["save"];
export const gameSaveInstructions = "请先在游戏内保存，再在退出时选择“存档并退出”。平台只提交游戏已写入的存档数据，不保存当前画面的即时进度；尚未在游戏内保存时，恢复位置可能与当前画面不同。恢复后请从游戏菜单读档。不选存档启动会干净重开，旧存档和本地草稿均不会自动载入。";

export function nativeSaveInstructions(save: NativeSaveCapabilities) {
  if (!save || save.capture === "IN_GAME" && save.restore === "IN_GAME") {return gameSaveInstructions;}
  const capture = save.capture === "RUNTIME"
    ? save.captureAvailable ? "点击“创建存档”可保存当前游戏进度。" : "当前场景暂时无法创建存档，请继续游戏后重试。"
    : "请先在游戏内保存，再在退出时选择“存档并退出”。";
  const restore = save.restore === "AUTOMATIC" ? "选择这次创建的存档启动后，会自动恢复到其记录的位置。" : "恢复后请从游戏菜单读档。";
  return `${capture}平台保存游戏原生存档。${restore}不选存档启动会干净重开。`;
}

export function checkpointSyncText(semantics: CheckpointSemantics, tone: string, text: string) {
  return semantics === "GAME_SAVE" && tone === "synced" && text === "可创建存档" ? "游戏数据变化后会暂存在此浏览器" : text;
}
