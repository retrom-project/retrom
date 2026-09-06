import type {RuntimeSavePayload} from "./runtime/runtime-actions";

export type GameSaveDraft = {
  userId: string; launchId: string; title: string; restored: boolean;
  updatedAtMs: number; payload: RuntimeSavePayload;
};
export type GameSaveDraftStore = {
  put: (payload: RuntimeSavePayload) => Promise<void>;
  remove: () => Promise<void>;
};

const databaseName = "retrom-game-save-drafts-v1";
const storeName = "drafts";

async function openDatabase() {
  return new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open(databaseName, 1);
    let blocked = false;
    request.onupgradeneeded = () => {
      request.result.createObjectStore(storeName, {keyPath: ["userId", "launchId"]});
    };
    request.onsuccess = () => {
      if (blocked) {request.result.close();} else {resolve(request.result);}
    };
    request.onerror = () => reject(request.error ?? new Error("LOCAL_DRAFT_STORAGE_FAILED"));
    request.onblocked = () => {blocked = true; reject(new Error("LOCAL_DRAFT_STORAGE_BLOCKED"));};
  });
}

async function transaction<T>(mode: IDBTransactionMode, action: (store: IDBObjectStore) => IDBRequest<T>) {
  const database = await openDatabase();
  try {
    return await new Promise<T>((resolve, reject) => {
      const tx = database.transaction(storeName, mode);
      const request = action(tx.objectStore(storeName));
      tx.oncomplete = () => resolve(request.result);
      tx.onabort = () => reject(tx.error ?? new Error("LOCAL_DRAFT_STORAGE_FAILED"));
      tx.onerror = () => reject(tx.error ?? new Error("LOCAL_DRAFT_STORAGE_FAILED"));
    });
  } finally {database.close();}
}

/** This store never supplies a runtime restore payload. Launch restore is explicit. */
export function gameSaveDraftStore(identity: Omit<GameSaveDraft, "payload" | "updatedAtMs">): GameSaveDraftStore {
  return {
    put: async (payload) => {await transaction("readwrite", (store) => store.put({...identity, payload, updatedAtMs: Date.now()}));},
    remove: () => deleteGameSaveDraft(identity.userId, identity.launchId),
  };
}

export async function deleteGameSaveDraft(userId: string, launchId: string, expectedRequestId?: string) {
  if (!expectedRequestId) {await transaction("readwrite", (store) => store.delete([userId, launchId])); return;}
  await transaction("readwrite", (store) => {
    const request: IDBRequest<GameSaveDraft | undefined> = store.get([userId, launchId]);
    request.onsuccess = () => {
      if (request.result && request.result.payload.requestId !== expectedRequestId) {request.transaction?.abort(); return;}
      store.delete([userId, launchId]);
    };
    return request;
  });
}

export async function listGameSaveDrafts(userId: string): Promise<GameSaveDraft[]> {
  const drafts: GameSaveDraft[] = await transaction("readonly", (store) => store.getAll(IDBKeyRange.bound([userId], [userId, []])));
  return drafts.sort((a, b) => b.updatedAtMs - a.updatedAtMs);
}
