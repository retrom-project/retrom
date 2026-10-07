import type * as SaveDraftModule from "./save-drafts";
import { act, cleanup, screen } from "@testing-library/react";
import { renderHook } from "@/components/toast-test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSaveSession } from "./use-save-session";
import { disposeRuntime } from "./runtime/dispose-runtime";
import { checkpoint, runFixture, runtimeFixture } from "./player-test-fixture";
import { makeDraft, putDraft, uploadDraft } from "./save-drafts";
vi.mock("./save-drafts", async () => {
  const original =
    await vi.importActual<typeof SaveDraftModule>("./save-drafts");
  return { ...original, putDraft: vi.fn(), uploadDraft: vi.fn() };
});
beforeEach(() => vi.clearAllMocks());
afterEach(cleanup);
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
    expect(screen.getByRole("status")).toHaveTextContent("存档已同步。");
    expect(result.current.status).toBe("");
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

it("acknowledges J2ME RMS only after delayed persistence and serializes automatic EXPORT", async () => {
  const run = runFixture();
  run.extinfo.checkpointFormat = "j2me-rms-bundle-v1-storage-v1";
  const rms = { ...checkpoint, format: run.extinfo.checkpointFormat };
  const runtime = runtimeFixture();
  const capture = vi.spyOn(runtime, "checkpoint").mockImplementation(async (request) => {
    expect(request?.intent).toBe("EXPORT");
    return rms;
  });
  const availability = vi.spyOn(runtime, "getCheckpointAvailability");
  const acknowledge = vi.spyOn(runtime, "acknowledgeCheckpoint").mockImplementation(async () => {
    availability.mockReturnValue({ available: false, reason: "UNCHANGED" });
  });
  let release!: (value: Awaited<ReturnType<typeof uploadDraft>>) => void;
  vi.mocked(uploadDraft).mockImplementationOnce(() => new Promise((resolve) => { release = resolve; }));
  const { result } = renderHook(() => useSaveSession(run, "user-a", true));
  await act(async () => { result.current.nativeChanged(runtime); });
  expect(result.current.busy).toBe(true);
  expect(putDraft).toHaveBeenCalledOnce();
  expect(acknowledge).not.toHaveBeenCalled();
  await act(async () => { result.current.nativeChanged(runtime); });
  expect(capture).toHaveBeenCalledOnce();
  await act(async () => { release({ id: "rms-save", version: 1 } as Awaited<ReturnType<typeof uploadDraft>>); });
  expect(uploadDraft).toHaveBeenCalledOnce();
  expect(acknowledge).toHaveBeenCalledWith(rms);
  expect(acknowledge).toHaveBeenCalledOnce();
  expect(result.current.busy).toBe(false);
  expect(result.current.draft).toBeNull();
});
it("retains the same native commit when acknowledgement fails after durable persistence", async () => {
  const runtime = runtimeFixture();
  const acknowledge = vi.spyOn(runtime, "acknowledgeCheckpoint").mockRejectedValueOnce(new Error("确认失败"));
  vi.spyOn(runtime, "getCheckpointAvailability").mockReturnValue({ available: false, reason: "UNCHANGED" });
  vi.mocked(uploadDraft).mockResolvedValue({ id: "native-id", version: 4 } as Awaited<ReturnType<typeof uploadDraft>>);
  const { result } = renderHook(() => useSaveSession(runFixture(), "user-a", true));
  await act(() => result.current.capture(runtime, "EXPORT"));
  const draft = result.current.draft;
  expect(draft).not.toBeNull();
  expect(putDraft).toHaveBeenCalledTimes(2);
  expect(screen.queryByRole("status")).not.toBeInTheDocument();
  expect(result.current.status).toBe("确认失败");
  expect(result.current.currentSave?.version).toBe(4);
  await act(() => result.current.retry(runtime));
  expect(vi.mocked(uploadDraft).mock.calls[1][0].metadata.commitId).toBe(draft?.metadata.commitId);
  expect(acknowledge).toHaveBeenCalledTimes(2);
  expect(result.current.currentSave?.version).toBe(4);
  expect(result.current.draft).toBeNull();
  expect(screen.getByRole("status")).toHaveTextContent("存档已同步。");
});

it.each([true, false])("waits for the pending native save before unmount (persistence success=%s)", async (succeeds) => {
  const runtime = runtimeFixture();
  const capture = vi.spyOn(runtime, "checkpoint");
  const exit = vi.spyOn(runtime, "exit");
  const availability = vi.spyOn(runtime, "getCheckpointAvailability");
  const acknowledge = vi.spyOn(runtime, "acknowledgeCheckpoint").mockImplementation(async () => {
    availability.mockReturnValue({ available: false, reason: "UNCHANGED" });
  });
  let release!: () => void;
  vi.mocked(uploadDraft).mockImplementationOnce(() => new Promise((resolve, reject) => {
    release = () => succeeds ? resolve({ id: "native-id", version: 1 } as Awaited<ReturnType<typeof uploadDraft>>) : reject(new Error("离线"));
  }));
  const { result } = renderHook(() => useSaveSession(runFixture(), "user-a", true));
  await act(async () => { result.current.nativeChanged(runtime); });
  const draftId = result.current.draft?.id;
  let teardown!: Promise<void>;
  await act(async () => {
    teardown = disposeRuntime(runtime, async (snapshot) => {
      if (!await result.current.commit(snapshot, null)) { throw new Error("草稿尚未同步"); }
    }, result.current.settle);
  });
  expect(capture).toHaveBeenCalledOnce();
  expect(exit).not.toHaveBeenCalled();
  await act(async () => {
    release();
    if (succeeds) { await teardown; }
    else { await expect(teardown).rejects.toThrow("草稿尚未同步"); }
  });
  expect(uploadDraft).toHaveBeenCalledOnce();
  expect(putDraft).toHaveBeenCalledOnce();
  expect(acknowledge).toHaveBeenCalledTimes(succeeds ? 1 : 0);
  expect(exit).toHaveBeenCalledOnce();
  if (succeeds) { expect(result.current.draft).toBeNull(); }
  else { expect(result.current.draft?.id).toBe(draftId); }
});
