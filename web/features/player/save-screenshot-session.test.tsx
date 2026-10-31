import type * as SaveDraftModule from "./save-drafts";
import { act, cleanup } from "@testing-library/react";
import { renderHook } from "@/components/toast-test-utils";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useSaveSession } from "./use-save-session";
import { checkpoint, runFixture, runtimeFixture } from "./player-test-fixture";
import { putDraft, uploadDraft } from "./save-drafts";
import { prepareSaveScreenshot } from "./save-screenshot";

vi.mock("./runtime/provider-dispatcher", () => ({ reclaimProviderContentCache: vi.fn() }));
vi.mock("./save-screenshot", () => ({ prepareSaveScreenshot: vi.fn() }));
vi.mock("./save-drafts", async () => {
  const original = await vi.importActual<typeof SaveDraftModule>("./save-drafts");
  return { ...original, putDraft: vi.fn(), uploadDraft: vi.fn() };
});

const source = new Blob(["source"], { type: "image/png" });
const jpeg = new Blob(["compressed"], { type: "image/jpeg" });
beforeEach(() => {
  vi.resetAllMocks();
  vi.mocked(prepareSaveScreenshot).mockResolvedValue(jpeg);
  vi.mocked(uploadDraft).mockResolvedValue({ id: "save-id", version: 1 } as Awaited<ReturnType<typeof uploadDraft>>);
});
afterEach(cleanup);

it.each([false, true])("retains and uploads the encoded screenshot for native=%s", async native => {
  const runtime = runtimeFixture();
  vi.spyOn(runtime, "screenshot").mockResolvedValue(source);
  const { result } = renderHook(() => useSaveSession(runFixture(), "user-a", native));
  await act(() => result.current.capture(runtime, native ? "EXPORT" : "CAPTURE"));
  expect(prepareSaveScreenshot).toHaveBeenCalledExactlyOnceWith(source);
  expect(vi.mocked(putDraft).mock.calls[0][0].screenshot).toBe(jpeg);
  expect(vi.mocked(uploadDraft).mock.calls[0][0].screenshot).toBe(jpeg);
});

it("encodes a checkpoint's own save-point image instead of taking another frame", async () => {
  const runtime = runtimeFixture();
  vi.spyOn(runtime, "checkpoint").mockResolvedValue({ ...checkpoint, screenshot: source });
  const capture = vi.spyOn(runtime, "screenshot");
  const { result } = renderHook(() => useSaveSession(runFixture(), "user-a", true));
  await act(() => result.current.capture(runtime, "EXPORT"));
  expect(capture).not.toHaveBeenCalled();
  expect(prepareSaveScreenshot).toHaveBeenCalledExactlyOnceWith(source);
  expect(vi.mocked(putDraft).mock.calls[0][0].screenshot).toBe(jpeg);
});

it("encodes final native snapshots after the runtime has exited", async () => {
  const { result } = renderHook(() => useSaveSession(runFixture(), "user-a", true));
  await act(() => result.current.commit({ checkpoint, screenshot: source }, null));
  expect(prepareSaveScreenshot).toHaveBeenCalledExactlyOnceWith(source);
  expect(vi.mocked(putDraft).mock.calls[0][0].screenshot).toBe(jpeg);
});

it("encodes automatic native saves through the same draft boundary", async () => {
  const runtime = runtimeFixture();
  vi.spyOn(runtime, "screenshot").mockResolvedValue(source);
  const { result } = renderHook(() => useSaveSession(runFixture(), "user-a", true));
  await act(async () => { result.current.nativeChanged(runtime); await result.current.settle(); });
  expect(prepareSaveScreenshot).toHaveBeenCalledExactlyOnceWith(source);
  expect(vi.mocked(uploadDraft).mock.calls[0][0].screenshot).toBe(jpeg);
});

it("reuses the encoded draft when upload is retried", async () => {
  const runtime = runtimeFixture();
  vi.spyOn(runtime, "screenshot").mockResolvedValue(source);
  vi.spyOn(runtime, "getCheckpointAvailability").mockReturnValue({ available: false, reason: "UNCHANGED" });
  vi.mocked(uploadDraft).mockRejectedValueOnce(new Error("offline"));
  const { result } = renderHook(() => useSaveSession(runFixture(), "user-a", true));
  await act(() => result.current.capture(runtime, "EXPORT"));
  const original = result.current.draft;
  await act(() => result.current.retry(runtime));
  expect(prepareSaveScreenshot).toHaveBeenCalledOnce();
  expect(vi.mocked(uploadDraft).mock.calls.map(([draft]) => draft)).toEqual([original, original]);
  expect(original?.screenshot).toBe(jpeg);
});

it("preserves checkpoint bytes and existing-image semantics if encoding is unavailable", async () => {
  vi.mocked(prepareSaveScreenshot).mockResolvedValue(null);
  const { result } = renderHook(() => useSaveSession(runFixture(), "user-a", true));
  await act(() => result.current.commit({ checkpoint, screenshot: source }, null));
  const draft = vi.mocked(putDraft).mock.calls[0][0];
  expect(draft.screenshot).toBeNull();
  expect(new Uint8Array(await draft.payload.arrayBuffer())).toEqual(checkpoint.bytes);
  expect(uploadDraft).toHaveBeenCalledOnce();
});
