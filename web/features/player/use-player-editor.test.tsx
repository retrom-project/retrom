import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { PlayerRuntimeV1, RuntimeGameEditorV1, RuntimeStateV1 } from "./runtime/contract";
import { usePlayerEditor } from "./use-player-editor";

afterEach(() => { cleanup(); document.body.replaceChildren(); });

function setup(state: RuntimeStateV1 = "RUNNING", available = true) {
  const editor = { categories: vi.fn(), entries: vi.fn(), set: vi.fn() } as RuntimeGameEditorV1;
  const pause = vi.fn(async () => {});
  const runtime = { current: {
    getState: () => state, getCapabilities: () => ({ pause: true }),
    getGameEditor: () => available ? editor : null, pause,
  } as unknown as PlayerRuntimeV1 };
  const onError = vi.fn();
  const hook = renderHook(() => usePlayerEditor(runtime, state, onError));
  return { ...hook, pause, editor, onError };
}

it.each(["MOUNTING", "EXITED"] as const)("does not offer an editor before or after playable state %s", async (state) => {
  const { result, pause } = setup(state);
  expect(result.current.editor).toBeNull();
  await act(() => result.current.show());
  expect(pause).not.toHaveBeenCalled();
  expect(result.current.open).toBe(false);
});

it("does not offer an editor for a runtime without that public capability", async () => {
  const { result, pause } = setup("RUNNING", false);
  await act(() => result.current.show());
  expect(result.current.editor).toBeNull();
  expect(pause).not.toHaveBeenCalled();
});

it("waits for pause once, opens the public editor and returns focus to more while staying paused", async () => {
  const more = document.createElement("button"); more.id = "player-more-button"; document.body.append(more);
  const { result, pause, editor } = setup();
  let resolve!: () => void;
  pause.mockImplementation(() => new Promise<void>((done) => { resolve = done; }));
  let opening!: Promise<void>;
  act(() => { opening = result.current.show(); void result.current.show(); });
  expect(result.current.pending).toBe(true);
  expect(result.current.open).toBe(false);
  expect(pause).toHaveBeenCalledOnce();
  await act(async () => { resolve(); await opening; });
  expect(result.current.editor).toBe(editor);
  expect(result.current.open).toBe(true);
  act(() => result.current.close());
  expect(more).toHaveFocus();
});

it("keeps an already paused immersive game paused and reports a failed pause without opening", async () => {
  const paused = setup("PAUSED");
  await act(() => paused.result.current.show());
  expect(paused.pause).not.toHaveBeenCalled();
  expect(paused.result.current.open).toBe(true);
  paused.unmount();
  const running = setup();
  running.pause.mockRejectedValue(new Error("暂停失败"));
  await act(() => running.result.current.show());
  expect(running.result.current.open).toBe(false);
  expect(running.result.current.pending).toBe(false);
  expect(running.onError).toHaveBeenCalledWith("暂停失败");
});
