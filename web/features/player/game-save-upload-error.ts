export class GameSaveConflict extends Error {
  constructor() {super("存档已被其他会话更新或删除，本次改动未同步。请退出后重新选择存档。");}
}

export function isGameSaveConflict(body: string) {
  try {
    const value: unknown = JSON.parse(body);
    if (!value || typeof value !== "object" || !("error" in value)) {return false;}
    const error = value.error;
    return Boolean(error && typeof error === "object" && "code" in error && error.code === "SAVE_SYNC_CONFLICT");
  } catch {return false;}
}
