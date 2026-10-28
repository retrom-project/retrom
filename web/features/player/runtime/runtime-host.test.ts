import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, expect, it, vi } from "vitest";
import type { LaunchEnvelopeV1 } from "./contract";
import { createRuntimeHost } from "./runtime-host";

vi.mock("./isolated-frame", () => ({
  prepareIsolatedFrame: vi.fn(async (frame: HTMLIFrameElement) => {
    expectDialogPermission(frame);
    frame.src = "https://isolated.retrom.invalid/shell.html";
  }),
}));
vi.mock("./blank-frame", () => ({
  prepareBlankFrame: vi.fn(async (frame: HTMLIFrameElement, target: HTMLElement) => {
    expectDialogPermission(frame);
    target.append(frame);
  }),
}));

afterEach(() => {
  vi.clearAllMocks();
  document.body.replaceChildren();
});

function expectDialogPermission(frame: HTMLIFrameElement) {
  const permissions = frame.getAttribute("sandbox")?.split(/\s+/u) ?? [];
  expect(permissions).toContain("allow-modals");
  expect(permissions).not.toContain("allow-popups");
  expect(permissions).not.toContain("allow-top-navigation");
}

it.each(["ISOLATED_ORIGIN_RESOURCE", "SAME_ORIGIN_BLANK"] as const)(
  "permits game confirmation dialogs before the %s navigation",
  async (frameMode) => {
    const envelope = JSON.parse(readFileSync(resolve(
      process.cwd(), "../api/runtime-provider/v1/fixtures/valid/isolated-web.json",
    ), "utf8")) as LaunchEnvelopeV1;
    envelope.runtime.capabilities.frameMode = frameMode;
    const controller = new AbortController();
    const target = document.createElement("div");
    document.body.append(target);
    try {
      const mounted = await createRuntimeHost(envelope, controller.signal).mountFrame(
        target, { resourceRole: frameMode === "SAME_ORIGIN_BLANK" ? null : "game" },
      );
      expectDialogPermission(mounted.element);
      expect(mounted.element.referrerPolicy).toBe("no-referrer");
    } finally {
      controller.abort();
    }
    expect(target.childElementCount).toBe(0);
  },
);
it("passes the per-core touch gamepad preference before a runtime mounts", () => {
  const envelope = JSON.parse(readFileSync(resolve(
    process.cwd(), "../api/runtime-provider/v1/fixtures/valid/single-minimal.json",
  ), "utf8")) as LaunchEnvelopeV1;
  localStorage.clear();
  const key = `retrom:touch-gamepad:${envelope.runtime.coreId}`;
  expect(createRuntimeHost(envelope, new AbortController().signal).touchGamepadEnabled).toBe(true);
  localStorage.setItem(key, "off");
  expect(createRuntimeHost(envelope, new AbortController().signal).touchGamepadEnabled).toBe(false);
  localStorage.clear();
});
