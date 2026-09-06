export type CheckpointSemantics = "INSTANT" | "GAME_SAVE";
export const gameSaveInstructions = "此类游戏不支持即时存档。请在游戏内保存，平台会自动同步游戏数据；恢复后请从游戏菜单读档。截图展示最近同步时的画面。从已有存档启动会持续更新该存档；直接开始游戏会建立独立存档。";

export function checkpointSyncText(semantics: CheckpointSemantics, tone: string, text: string) {
  return semantics === "GAME_SAVE" && tone === "synced" && text === "可创建存档" ? "游戏数据变化后会自动同步" : text;
}
