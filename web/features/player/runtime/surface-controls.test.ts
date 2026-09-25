import {describe, expect, it, vi} from "vitest";

import type {PlayerRuntimeV1} from "./contract";
import {installRuntimeSurfaceControls} from "./surface-controls";

describe("Provider-neutral runtime surface controls", () => {
  it("forwards the standard pause shortcut from the runtime frame to the Host", () => {
    const frame = document.createElement("iframe");
    document.body.append(frame);
    const frameWindow = frame.contentWindow!;
    const canvas = frameWindow.document.createElement("canvas");
    frameWindow.document.body.append(canvas);
    const onKeyboardPause = vi.fn();
    const cleanup = installRuntimeSurfaceControls(runtime(canvas), {
      experience: "standard", onKeyboardPause, onImmersiveMenuShortcut: vi.fn(),
      onRevealControls: vi.fn(), onShowControls: vi.fn(), onSurface: vi.fn(),
    });

    frameWindow.document.body.dispatchEvent(new KeyboardEvent("keydown", {
      bubbles: true, code: "KeyP", key: "p",
    }));

    expect(onKeyboardPause).toHaveBeenCalledOnce();
    cleanup();
  });

  it("leaves P available to computer cores that need a full keyboard", () => {
    const frame = document.createElement("iframe");
    document.body.append(frame);
    const frameWindow = frame.contentWindow!;
    const canvas = frameWindow.document.createElement("canvas");
    frameWindow.document.body.append(canvas);
    const onKeyboardPause = vi.fn();
    const cleanup = installRuntimeSurfaceControls(runtime(canvas), {
      experience: "standard", keyboardPauseShortcut: false, onKeyboardPause,
      onImmersiveMenuShortcut: vi.fn(), onRevealControls: vi.fn(),
      onShowControls: vi.fn(), onSurface: vi.fn(),
    });
    const event = new KeyboardEvent("keydown", {bubbles: true, cancelable: true, code: "KeyP", key: "p"});
    canvas.dispatchEvent(event);
    expect(onKeyboardPause).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
    cleanup();
  });

  it("forwards the immersive menu shortcut without leaking Provider DOM", () => {
    const frame = document.createElement("iframe");
    document.body.append(frame);
    const frameWindow = frame.contentWindow!;
    const canvas = frameWindow.document.createElement("canvas");
    frameWindow.document.body.append(canvas);
    const onImmersiveMenuShortcut = vi.fn();
    const cleanup = installRuntimeSurfaceControls(runtime(canvas), {
      experience: "immersive", onKeyboardPause: vi.fn(), onImmersiveMenuShortcut,
      onRevealControls: vi.fn(), onShowControls: vi.fn(), onSurface: vi.fn(),
    });

    frameWindow.document.body.dispatchEvent(new KeyboardEvent("keydown", {
      bubbles: true, code: "KeyM", key: "m",
    }));

    expect(onImmersiveMenuShortcut).toHaveBeenCalledOnce();
    cleanup();
  });
});

function runtime(canvas: HTMLCanvasElement) {
  return {getCanvas: () => canvas} as PlayerRuntimeV1;
}
