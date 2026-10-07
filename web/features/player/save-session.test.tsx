import type * as SaveDraftModule from "./save-drafts";
import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useSaveSession } from "./use-save-session";
import { checkpoint, runFixture, runtimeFixture } from "./player-test-fixture";
import { makeDraft, putDraft, uploadDraft } from "./save-drafts";
vi.mock("./save-drafts", async () => {
  const original =
    await vi.importActual<typeof SaveDraftModule>("./save-drafts");
  return { ...original, putDraft: vi.fn(), uploadDraft: vi.fn() };
});
beforeEach(() => vi.clearAllMocks());
describe("durable native save", () => {
  it("keeps exact startup identity and does not acknowledge a context-expired commit", async () => {
    const run = runFixture();
    const runtime = runtimeFixture();
    const acknowledge = vi.spyOn(runtime, "acknowledgeCheckpoint");
    vi.mocked(uploadDraft).mockRejectedValue(new Error("运行上下文已过期。"));
    const { result } = renderHook(() => useSaveSession(run, "user-a", true));
    await act(() => result.current.capture(runtime, "EXPORT"));
    expect(putDraft).toHaveBeenCalledOnce();
    expect(result.current.draft?.metadata.extinfo).toEqual(run.extinfo);
    expect(result.current.draft?.userId).toBe("user-a");
    expect(acknowledge).not.toHaveBeenCalled();
    expect(result.current.status).toContain("过期");
  });
  it("retries the same commit and acknowledges only after successful server persistence", async () => {
    const runtime = runtimeFixture();
    const acknowledge = vi.spyOn(runtime, "acknowledgeCheckpoint");
    vi.spyOn(runtime, "getCheckpointAvailability").mockReturnValue({
      available: false,
      reason: "UNCHANGED",
    });
    vi.mocked(uploadDraft).mockRejectedValueOnce(new Error("响应中断"));
    const { result } = renderHook(() =>
      useSaveSession(runFixture(), "user-a", true),
    );
    await act(() => result.current.capture(runtime, "EXPORT"));
    const draft = result.current.draft;
    expect(draft).not.toBeNull();
    vi.mocked(uploadDraft).mockResolvedValueOnce({
      id: "native-id",
      version: 2,
    } as Awaited<ReturnType<typeof uploadDraft>>);
    await act(() => result.current.retry(runtime));
    expect(vi.mocked(uploadDraft).mock.calls[1][0].metadata.commitId).toBe(
      draft?.metadata.commitId,
    );
    expect(acknowledge).toHaveBeenCalledWith(checkpoint);
    expect(result.current.draft).toBeNull();
  });
  it("synchronizes a newer native revision after retry without requiring another event", async () => {
    const run = runFixture();
    const runtime = runtimeFixture();
    const newer = {
      ...checkpoint,
      bytes: new Uint8Array([4, 5, 6]),
      metadata: { revision: "newer" },
    };
    const availability = vi.spyOn(runtime, "getCheckpointAvailability");
    availability.mockReturnValue({
      available: true,
      reason: null,
      revision: "newer",
    });
    const acknowledge = vi
      .spyOn(runtime, "acknowledgeCheckpoint")
      .mockImplementation(async (captured) => {
        if (captured.metadata?.revision === "newer") {
          availability.mockReturnValue({
            available: false,
            reason: "UNCHANGED",
          });
        }
      });
    vi.mocked(uploadDraft)
      .mockRejectedValueOnce(new Error("离线"))
      .mockResolvedValueOnce({ id: "native-id", version: 1 } as Awaited<
        ReturnType<typeof uploadDraft>
      >)
      .mockResolvedValueOnce({ id: "native-id", version: 2 } as Awaited<
        ReturnType<typeof uploadDraft>
      >);
    const { result } = renderHook(() => useSaveSession(run, "user-a", true));
    await act(() => result.current.capture(runtime, "EXPORT"));
    const original = result.current.draft;
    vi.spyOn(runtime, "checkpoint").mockResolvedValue(newer);
    await act(() => result.current.retry(runtime));
    expect(uploadDraft).toHaveBeenCalledTimes(3);
    const retried = vi.mocked(uploadDraft).mock.calls[1][0];
    const latest = vi.mocked(uploadDraft).mock.calls[2][0];
    expect(retried.metadata.commitId).toBe(original?.metadata.commitId);
    expect(latest.metadata.commitId).not.toBe(retried.metadata.commitId);
    expect(latest.saveId).toBe("native-id");
    expect(latest.metadata.version).toBe(1);
    expect(latest.metadata.extinfo).toEqual(run.extinfo);
    expect(
      acknowledge.mock.calls.map(([value]) => value.metadata?.revision),
    ).toEqual(["original", "newer"]);
    expect(result.current.draft).toBeNull();
  });
  it("keeps review checkpoints in memory without durable save traffic", async () => {
    const run = { ...runFixture(), purpose: "review" as const };
    const { result } = renderHook(() => useSaveSession(run, "admin", true));
    await act(() => result.current.capture(runtimeFixture()));
    expect(result.current.preview?.checkpoint).toEqual(checkpoint);
    expect(putDraft).not.toHaveBeenCalled();
    expect(uploadDraft).not.toHaveBeenCalled();
  });
  it("finishes an instant checkpoint without native acknowledgement", async () => {
    const runtime = runtimeFixture();
    const acknowledge = vi
      .spyOn(runtime, "acknowledgeCheckpoint")
      .mockRejectedValue(new Error("PLAYER_RUNTIME_CAPABILITY_UNSUPPORTED"));
    vi.mocked(uploadDraft).mockResolvedValueOnce({
      id: "instant-id",
      version: 1,
    } as Awaited<ReturnType<typeof uploadDraft>>);
    const { result } = renderHook(() =>
      useSaveSession(runFixture(), "user-a", false),
    );
    await act(() => result.current.capture(runtime));
    expect(uploadDraft).toHaveBeenCalledOnce();
    expect(acknowledge).not.toHaveBeenCalled();
    expect(result.current.draft).toBeNull();
    expect(result.current.status).toBe("存档已同步。");
  });
  it("keeps instant new and overwrite choices distinct and freezes the chosen version", async () => {
    const runtime = runtimeFixture();
    const acknowledge = vi.spyOn(runtime, "acknowledgeCheckpoint");
    const saved = {
      id: "instant-id",
      name: "我的进度",
      slot: "slot-2",
      version: 1,
    } as NonNullable<ReturnType<typeof runFixture>["save"]>;
    vi.mocked(uploadDraft)
      .mockResolvedValueOnce(saved)
      .mockResolvedValueOnce({ ...saved, version: 2 })
      .mockResolvedValueOnce({ ...saved, id: "new-id" });
    const { result } = renderHook(() =>
      useSaveSession(runFixture(), "user-a", false),
    );
    await act(() => result.current.capture(runtime));
    expect(vi.mocked(uploadDraft).mock.calls[0][0].saveId).toBeNull();
    await act(() =>
      result.current.capture(runtime, "CAPTURE", result.current.currentSave),
    );
    const overwrite = vi.mocked(uploadDraft).mock.calls[1][0];
    expect(overwrite.saveId).toBe("instant-id");
    expect(overwrite.metadata).toMatchObject({
      name: "我的进度",
      slot: "slot-2",
      version: 1,
    });
    expect(result.current.currentSave?.version).toBe(2);
    await act(() => result.current.capture(runtime, "CAPTURE", null));
    const created = vi.mocked(uploadDraft).mock.calls[2][0];
    expect(created.saveId).toBeNull();
    expect(created.metadata.version).toBeUndefined();
    expect(acknowledge).not.toHaveBeenCalled();
  });
  it("freezes native overwrite id, name, slot, expected version and options", () => {
    const run = runFixture();
    const selected = {
      id: "selected-save",
      name: "我的命名",
      slot: "slot-2",
      version: 7,
    } as NonNullable<typeof run.save>;
    const draft = makeDraft("user-a", run, checkpoint, null, selected, true);
    run.extinfo.runtimeOptions.original = "changed";
    expect(draft.metadata).toMatchObject({
      name: "我的命名",
      slot: "slot-2",
      version: 7,
      extinfo: { runtimeOptions: { original: "value" } },
    });
    expect(draft.saveId).toBe("selected-save");
    expect(draft.metadata.commitId).toBe(draft.id);
  });
});
