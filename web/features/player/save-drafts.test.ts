import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { checkpoint, runFixture } from "./player-test-fixture";
import type { SaveDraft } from "./save-drafts";
import type * as DraftUploadModule from "./draft-upload";
import { DraftIdentityMismatch, uploadWithRenewal } from "./draft-upload";

vi.mock("./draft-upload", async (original) => ({
  ...await original<typeof DraftUploadModule>(),
  uploadWithRenewal: vi.fn(),
}));

// A request can succeed while its containing transaction aborts, as Chromium
// does when the shared origin quota is exhausted. Only oncomplete is durable.
function browserDatabase() {
  const records = new Map<string, SaveDraft>();
  const failures: (DOMException | null)[] = [];
  const puts: SaveDraft[] = [];
  const pendingAborts: (() => void)[] = [];
  const controls = { deferAbort: false };
  const db = {
    close: vi.fn(),
    transaction: vi.fn((_name: string, mode: IDBTransactionMode) => {
      const failure = mode === "readwrite" ? failures.shift() : null;
      let finish = () => {};
      const tx = {
        error: null as DOMException | null,
        onerror: null as null | (() => void),
        onabort: null as null | (() => void),
        oncomplete: null as null | (() => void),
        objectStore: () => ({
          put: (draft: SaveDraft) => {
            puts.push(draft);
            finish = () => { records.set(draft.id, draft); };
            return { result: draft.id };
          },
          get: (id: string) => ({ result: records.get(id) }),
          getAll: () => ({ result: [...records.values()] }),
          delete: (id: string) => {
            finish = () => { records.delete(id); };
            return { result: undefined };
          },
        }),
      };
      queueMicrotask(() => {
        if (failure) {
          tx.onerror?.();
          const abort = () => { tx.error = failure; tx.onabort?.(); };
          if (controls.deferAbort) { pendingAborts.push(abort); }
          else { queueMicrotask(abort); }
        }
        else { finish(); tx.oncomplete?.(); }
      });
      return tx;
    }),
  };
  vi.stubGlobal("indexedDB", {
    open: () => {
      const request = { result: db, onsuccess: null as null | (() => void) };
      queueMicrotask(() => request.onsuccess?.());
      return request;
    },
  });
  return { records, failures, puts, db, pendingAborts, controls };
}

beforeEach(() => { vi.resetModules(); vi.clearAllMocks(); });
afterEach(() => vi.unstubAllGlobals());

async function fixture() {
  const browser = browserDatabase();
  const saves = await import("./save-drafts");
  const draft = saves.makeDraft("owner", runFixture(), checkpoint, null, null, true);
  return { ...browser, ...saves, draft };
}

describe("durable save under shared content-cache quota pressure", () => {
  it("waits for native transaction abort before closing its connection or attempting recovery", async () => {
    const f = await fixture();
    f.controls.deferAbort = true;
    f.failures.push(new DOMException("Origin full", "QuotaExceededError"), null);
    const reclaim = vi.fn().mockResolvedValue({ reclaimedBytes: 1, removedObjects: 1 });
    let settled = false;
    const operation = f.putDraft(f.draft, reclaim).catch((error: unknown) => error).finally(() => { settled = true; });
    await vi.waitFor(() => expect(f.pendingAborts).toHaveLength(1));
    expect(settled).toBe(false);
    expect(f.db.close).not.toHaveBeenCalled();
    expect(reclaim).not.toHaveBeenCalled();
    f.pendingAborts.shift()?.();
    expect(await operation).toBeUndefined();
    expect(reclaim).toHaveBeenCalledOnce();
  });

  it("reclaims only after a quota abort and retries the exact draft once", async () => {
    const f = await fixture();
    f.failures.push(new DOMException("Origin full", "QuotaExceededError"), null);
    const reclaim = vi.fn().mockResolvedValue({ reclaimedBytes: 64 * 1024 * 1024, removedObjects: 1 });
    await f.putDraft(f.draft, reclaim);
    expect(reclaim).toHaveBeenCalledOnce();
    expect(reclaim).toHaveBeenCalledWith({ targetBytes: 32 * 1024 * 1024 });
    expect(f.puts).toEqual([f.draft, f.draft]);
    expect(await f.listDrafts("owner")).toEqual([f.draft]);
    f.records.clear();
    expect(await f.listDrafts("owner")).toEqual([]);
  });

  it("does not reclaim for a non-quota write failure and preserves its cause and exportable bytes", async () => {
    const f = await fixture();
    const failure = new DOMException("Cannot clone", "DataCloneError");
    f.failures.push(failure);
    const reclaim = vi.fn();
    await expect(f.putDraft(f.draft, reclaim)).rejects.toMatchObject({ cause: failure });
    expect(reclaim).not.toHaveBeenCalled();
    expect(f.puts).toEqual([f.draft]);
    expect(await f.listDrafts("owner")).toEqual([f.draft]);
    expect(await f.listDrafts("other-owner")).toEqual([]);
  });

  it("retains both quota failures and the same volatile commit when no space can be freed", async () => {
    const f = await fixture();
    const first = new DOMException("Quota before recovery", "QuotaExceededError");
    const second = new DOMException("Quota after recovery", "QuotaExceededError");
    f.failures.push(first, second);
    const reclaim = vi.fn().mockResolvedValue({ reclaimedBytes: 0, removedObjects: 0 });
    const failure = await f.putDraft(f.draft, reclaim).catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(Error);
    expect((failure as Error).cause).toBeInstanceOf(AggregateError);
    expect(((failure as Error).cause as AggregateError).errors).toEqual([first, second]);
    expect(reclaim).toHaveBeenCalledOnce();
    expect(f.puts).toHaveLength(2);
    expect(await f.listDrafts("owner")).toEqual([f.draft]);
  });

  it("retains the original quota error when cache recovery itself fails", async () => {
    const f = await fixture();
    const quota = new DOMException("Origin full", "QuotaExceededError");
    const recovery = new DOMException("Cache is unavailable", "SecurityError");
    f.failures.push(quota);
    const reclaim = vi.fn().mockRejectedValue(recovery);
    const failure = await f.putDraft(f.draft, reclaim).catch((error: unknown) => error);
    expect(((failure as Error).cause as AggregateError).errors).toEqual([quota, recovery]);
    expect(f.puts).toHaveLength(1);
    expect(await f.listDrafts("owner")).toEqual([f.draft]);
  });

  it("never reclaims during a successful write or without a verified Provider callback", async () => {
    const f = await fixture();
    const reclaim = vi.fn();
    await f.putDraft(f.draft, reclaim);
    expect(reclaim).not.toHaveBeenCalled();
    const quota = new DOMException("Origin full", "QuotaExceededError");
    f.failures.push(quota);
    await expect(f.putDraft(f.draft)).rejects.toMatchObject({ cause: quota });
    expect(reclaim).not.toHaveBeenCalled();
  });

  it("preserves the volatile commit if server persistence succeeds but local removal fails", async () => {
    const f = await fixture();
    f.failures.push(new DOMException("Origin full", "QuotaExceededError"));
    await expect(f.putDraft(f.draft)).rejects.toThrow("导出备份");
    f.records.set(f.draft.id, f.draft);
    f.failures.push(new DOMException("Local delete failed", "UnknownError"));
    vi.mocked(uploadWithRenewal).mockResolvedValue({ id: f.draft.id, version: 1 } as Awaited<ReturnType<typeof uploadWithRenewal>>);
    await expect(f.uploadDraft(f.draft, "owner")).rejects.toThrow("Local delete failed");
    // A new component still sees the same in-memory copy, even if IDB is gone.
    f.records.clear();
    expect(await f.listDrafts("owner")).toEqual([f.draft]);
    await f.uploadDraft(f.draft, "owner");
    expect(vi.mocked(uploadWithRenewal).mock.calls.map(([draft]) => draft.metadata.commitId)).toEqual([f.draft.id, f.draft.id]);
    expect(await f.listDrafts("owner")).toEqual([]);
  });

  it("uses the same quota recovery callback when persisting an identity mismatch", async () => {
    const f = await fixture();
    const mismatch = new DraftIdentityMismatch();
    vi.mocked(uploadWithRenewal).mockRejectedValue(mismatch);
    f.failures.push(new DOMException("Origin full", "QuotaExceededError"), null);
    const reclaim = vi.fn().mockResolvedValue({ reclaimedBytes: 1, removedObjects: 1 });
    await expect(f.uploadDraft(f.draft, "owner", reclaim)).rejects.toBe(mismatch);
    expect(reclaim).toHaveBeenCalledOnce();
    expect((await f.listDrafts("owner"))[0]).toMatchObject({ id: f.draft.id, recoveryError: mismatch.message });
  });
});
