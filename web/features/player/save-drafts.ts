import type { Schema } from "@/lib/api/types";
import { DraftIdentityMismatch, uploadWithRenewal } from "./draft-upload";
import type { RuntimeCheckpointV1 } from "./runtime/contract";
export type SaveDraft = {
  id: string;
  userId: string;
  gameId: string;
  gameTitle: string;
  createdAtMs: number;
  metadata: Schema<"SaveCommitMetadata">;
  saveId: string | null;
  payload: Blob;
  screenshot: Blob | null;
  checkpointMetadata: RuntimeCheckpointV1["metadata"];
  recoveryError?: string;
};
const volatileDrafts = new Map<string, SaveDraft>();
function database(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open("retrom-unsynced-saves", 1);
    request.onupgradeneeded = () =>
      request.result.createObjectStore("drafts", { keyPath: "id" });
    request.onerror = () => reject(request.error);
    request.onsuccess = () => resolve(request.result);
  });
}
async function transaction<T>(
  mode: IDBTransactionMode,
  action: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
  const db = await database();
  try {
    return await new Promise<T>((resolve, reject) => {
      const tx = db.transaction("drafts", mode);
      const request = action(tx.objectStore("drafts"));
      tx.oncomplete = () => resolve(request.result);
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error ?? new Error("本地存档写入失败。"));
    });
  } finally {
    db.close();
  }
}
export async function putDraft(draft: SaveDraft) {
  volatileDrafts.set(draft.id, draft);
  try {
    await transaction("readwrite", (store) => store.put(draft));
    volatileDrafts.delete(draft.id);
  } catch {
    throw new Error(
      "无法将存档写入浏览器。请先导出备份，刷新页面后将无法找回这份临时存档。",
    );
  }
}
export async function removeDraft(userId: string, id: string) {
  const volatile = volatileDrafts.get(id);
  if (volatile && volatile.userId !== userId) {
    throw new Error("无法访问其他账号的存档。");
  }
  volatileDrafts.delete(id);
  const record: unknown = await transaction("readonly", (store) =>
    store.get(id),
  );
  if (record === undefined) {
    return;
  }
  if (
    !record ||
    typeof record !== "object" ||
    !("userId" in record) ||
    record.userId !== userId
  ) {
    throw new Error("无法访问其他账号的存档。");
  }
  await transaction("readwrite", (store) => store.delete(id));
}
export async function listDrafts(userId: string): Promise<SaveDraft[]> {
  let records: unknown;
  try {
    records = await transaction("readonly", (store) => store.getAll());
  } catch {
    if (
      ![...volatileDrafts.values()].some((draft) => draft.userId === userId)
    ) {
      throw new Error(
        "无法读取浏览器中的未同步存档。请恢复浏览器存储权限后重试。",
      );
    }
    records = [];
  }
  if (!Array.isArray(records)) {
    throw new Error("本地存档读取失败。");
  }
  const unique = new Map(
    (records as SaveDraft[]).map((draft) => [draft.id, draft]),
  );
  for (const draft of volatileDrafts.values()) {
    unique.set(draft.id, draft);
  }
  return [...unique.values()]
    .filter((draft) => draft.userId === userId)
    .sort((a, b) => b.createdAtMs - a.createdAtMs);
}
export async function uploadDraft(draft: SaveDraft, currentUserId: string) {
  if (draft.userId !== currentUserId) {
    throw new Error("无法上传其他账号的存档。");
  }
  try {
    const save = await uploadWithRenewal(draft);
    await removeDraft(currentUserId, draft.id);
    return save;
  } catch (failure) {
    if (failure instanceof DraftIdentityMismatch) {
      draft.recoveryError = failure.message;
      await putDraft(draft);
    }
    throw failure;
  }
}
export function makeDraft(
  userId: string,
  run: Schema<"Run">,
  checkpoint: RuntimeCheckpointV1,
  screenshot: Blob | null,
  save: Schema<"Save"> | null,
  native: boolean,
): SaveDraft {
  const commitId = crypto.randomUUID();
  return {
    id: commitId,
    userId,
    gameId: run.gameId,
    gameTitle: runTitle(run),
    createdAtMs: Date.now(),
    saveId: save?.id ?? null,
    metadata: {
      commitId,
      runId: run.id,
      name: save?.name ?? (native ? "原生存档" : "手动存档"),
      kind: native ? "game_save" : "checkpoint",
      slot: save?.slot ?? null,
      extinfo: structuredClone(run.extinfo),
      ...(save ? { version: save.version } : {}),
    },
    payload: new Blob([Uint8Array.from(checkpoint.bytes).buffer], {
      type: "application/octet-stream",
    }),
    screenshot,
    checkpointMetadata: structuredClone(checkpoint.metadata),
  };
}
export function exportDraft(draft: SaveDraft) {
  const header = JSON.stringify({
    id: draft.id,
    userId: draft.userId,
    gameId: draft.gameId,
    gameTitle: draft.gameTitle,
    createdAtMs: draft.createdAtMs,
    metadata: draft.metadata,
    saveId: draft.saveId,
    checkpointMetadata: draft.checkpointMetadata,
    payloadSize: draft.payload.size,
    screenshotSize: draft.screenshot?.size ?? 0,
  });
  const backup = new Blob(
    [
      header,
      "\n",
      draft.payload,
      ...(draft.screenshot ? [draft.screenshot] : []),
    ],
    { type: "application/octet-stream" },
  );
  const link = document.createElement("a");
  const url = URL.createObjectURL(backup);
  link.href = url;
  link.download = `${draft.gameTitle}-${draft.metadata.kind}.retrom-save`;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

function runTitle(run: Schema<"Run">) {
  const session = run.envelope.session;
  return session && typeof session === "object" && "title" in session
    ? String(session.title)
    : "游戏存档";
}
