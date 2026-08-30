import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ReviewPreviewPlayer } from "./review-preview-player";

const adapter = vi.hoisted(() => ({
  mount: vi.fn(),
  capture: vi.fn(),
}));
const ons = vi.hoisted(() => ({
  create: vi.fn(),
  mount: vi.fn(),
  screenshot: vi.fn(),
  exit: vi.fn(),
  subscribe: vi.fn(() => vi.fn()),
}));
const kirikiri = vi.hoisted(() => ({
  create: vi.fn(),
  mount: vi.fn(),
  screenshot: vi.fn(),
  exit: vi.fn(),
  subscribe: vi.fn(() => vi.fn()),
}));

vi.mock("@/features/player/adapters/ejs-4.2.3-v2", () => ({
  mountEmulatorJS: adapter.mount,
  captureReviewScreenshot: adapter.capture,
}));
vi.mock("@/features/player/canvas-fit", () => ({
  installCanvasContain: () => ({ refresh: vi.fn(), cleanup: vi.fn() }),
}));
vi.mock("@xxxsen/retrom-runtime", () => ({
  createRuntime: vi.fn((config) => config.adapter.adapterKind === "ONS_YURI_WEB"
    ? ons.create(config)
    : kirikiri.create(config)),
}));

describe("ReviewPreviewPlayer", () => {
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
    adapter.mount.mockReset();
    adapter.capture.mockReset();
    ons.create.mockReset();
    ons.mount.mockReset();
    ons.screenshot.mockReset();
    ons.exit.mockReset();
    ons.subscribe.mockClear();
    kirikiri.create.mockReset();
    kirikiri.mount.mockReset();
    kirikiri.screenshot.mockReset();
    kirikiri.exit.mockReset();
    kirikiri.subscribe.mockClear();
  });

  it("captures and uploads a READY preview exactly five seconds after game start", async () => {
    vi.useFakeTimers();
    const emulator = { on: vi.fn(), takeScreenshot: vi.fn() };
    adapter.capture.mockResolvedValue({ screenshot: new Blob(["png"], { type: "image/png" }), format: "png" });
    adapter.mount.mockImplementation((_config, _target, callbacks) => {
      callbacks.onReady?.(emulator);
      callbacks.onGameStart?.();
      return vi.fn();
    });
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/config")) {return Promise.resolve(new Response(JSON.stringify({
        runtimeFamily: "EMULATORJS",
        gameTitle: "1944",
        reviewPreview: { importItemId: "item-1", captureAllowed: true, captureAfterMs: 5000 },
      }), { status: 200, headers: { "Content-Type": "application/json" } }));}
      if (url.endsWith("/review-screenshot") && init?.method === "POST") {
        return Promise.resolve(new Response(JSON.stringify({ screenshotId: "shot-1" }), { status: 201, headers: { "Content-Type": "application/json" } }));
      }
      throw new Error(`unexpected fetch ${url}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<ReviewPreviewPlayer previewId="preview-1" />);
    await act(async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve(); });
    expect(adapter.mount).toHaveBeenCalledOnce();
    expect(screen.getByText("游戏已启动，将在第 5 秒自动保存截图。")).toBeVisible();
    expect(adapter.capture).not.toHaveBeenCalled();

    await act(async () => { await vi.advanceTimersByTimeAsync(4_999); });
    expect(adapter.capture).not.toHaveBeenCalled();
    await act(async () => { await vi.advanceTimersByTimeAsync(1); });

    expect(adapter.capture).toHaveBeenCalledWith(emulator);
    expect(fetchMock).toHaveBeenCalledWith("/runtime/launches/preview-1/review-screenshot", expect.objectContaining({
      method: "POST", body: expect.any(Blob), headers: { "Content-Type": "image/png" },
    }));
    expect(screen.getByText("第 5 秒运行截图已保存；可以继续试玩。")).toBeVisible();
  });

  it("captures a blocked best-effort preview after five seconds", async () => {
    vi.useFakeTimers();
    const emulator = { on: vi.fn(), takeScreenshot: vi.fn() };
    adapter.capture.mockResolvedValue({ screenshot: new Blob(["png"], { type: "image/png" }), format: "png" });
    adapter.mount.mockImplementation((_config, _target, callbacks) => {
      callbacks.onReady?.(emulator);
      callbacks.onGameStart?.();
      return vi.fn();
    });
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      if (String(input).endsWith("/config")) {return Promise.resolve(new Response(JSON.stringify({
        runtimeFamily: "EMULATORJS",
        gameTitle: "Blocked game",
        reviewPreview: { importItemId: "item-2", captureAllowed: true, captureAfterMs: 5000 },
      }), { status: 200, headers: { "Content-Type": "application/json" } }));}
      return Promise.resolve(new Response(JSON.stringify({ screenshotId: "shot-blocked" }), {
        status: 201, headers: { "Content-Type": "application/json" },
      }));
    });
    vi.stubGlobal("fetch", fetchMock);

    render(<ReviewPreviewPlayer previewId="preview-2" />);
    await act(async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve(); });
    expect(screen.getByText("游戏已启动，将在第 5 秒自动保存截图。")).toBeVisible();
    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); });
    expect(adapter.capture).toHaveBeenCalledWith(emulator);
    expect(fetchMock).toHaveBeenCalledWith("/runtime/launches/preview-2/review-screenshot", expect.objectContaining({ method: "POST" }));
  });

  it("surfaces an asynchronous EmulatorJS iframe startup error", async () => {
    adapter.mount.mockImplementation(() => vi.fn());
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response(JSON.stringify({
      runtimeFamily: "EMULATORJS",
      gameTitle: "Broken game",
      reviewPreview: { importItemId: "item-3", captureAllowed: true, captureAfterMs: 5000 },
    }), { status: 200, headers: { "Content-Type": "application/json" } }))));

    render(<ReviewPreviewPlayer previewId="preview-3" />);
    await act(async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve(); });
    const frame = screen.getByTitle("Broken game 运行画面") as HTMLIFrameElement;
    act(() => {
      frame.contentWindow?.dispatchEvent(new ErrorEvent("error", { message: "archive worker rejected" }));
    });

    expect(screen.getByText("审核预览启动失败。")).toBeVisible();
    expect(screen.getByRole("alert")).toHaveTextContent("archive worker rejected");
  });

  it("fails instead of loading forever when EmulatorJS never starts", async () => {
    vi.useFakeTimers();
    adapter.mount.mockImplementation(() => vi.fn());
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response(JSON.stringify({
      runtimeFamily: "EMULATORJS",
      gameTitle: "Silent game",
      reviewPreview: { importItemId: "item-4", captureAllowed: true, captureAfterMs: 5000 },
    }), { status: 200, headers: { "Content-Type": "application/json" } }))));

    render(<ReviewPreviewPlayer previewId="preview-4" />);
    await act(async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve(); });
    await act(async () => { await vi.advanceTimersByTimeAsync(30_000); });

    expect(screen.getByText("审核预览启动失败。")).toBeVisible();
    expect(screen.getByRole("alert")).toHaveTextContent("启动超时");
  });

  it("mounts ONS, captures its canvas, and exits the runtime", async () => {
    vi.useFakeTimers();
    ons.mount.mockResolvedValue(undefined);
    ons.screenshot.mockResolvedValue(new Blob(["ons-png"], { type: "image/png" }));
    ons.exit.mockResolvedValue(undefined);
    ons.create.mockReturnValue({
      mount: ons.mount, screenshot: ons.screenshot, exit: ons.exit, subscribe: ons.subscribe,
    });
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).endsWith("/config")) {
        return Promise.resolve(new Response(JSON.stringify({
          runtimeFamily: "ONS", sessionId: "preview-ons", gameTitle: "ONS fixture",
          adapter: {
            adapterKind: "ONS_YURI_WEB", adapterId: "ons-yuri-web",
            runtimeBaseUrl: "/runtime/retrom-runtime/v0.3.7/",
            projectIndexUrl: `/runtime/content/project/${"a".repeat(64)}/index.json`,
            scriptEncoding: "utf8", checkpointSlot: 999,
          },
          reviewPreview: { importItemId: "item-ons", captureAllowed: true, captureAfterMs: 5000 },
        }), { status: 200, headers: { "Content-Type": "application/json" } }));
      }
      if (String(input).endsWith("/review-screenshot") && init?.method === "POST") {
        return Promise.resolve(new Response(JSON.stringify({ screenshotId: "shot-ons" }), {
          status: 201, headers: { "Content-Type": "application/json" },
        }));
      }
      throw new Error(`unexpected fetch ${String(input)}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const view = render(<ReviewPreviewPlayer previewId="preview-ons" />);
    await act(async () => { await Promise.resolve(); await Promise.resolve(); await Promise.resolve(); });

    expect(ons.create).toHaveBeenCalledOnce();
    expect(ons.mount).toHaveBeenCalledOnce();
    expect(screen.getByText("游戏已启动，将在第 5 秒自动保存截图。")).toBeVisible();
    await act(async () => { await vi.advanceTimersByTimeAsync(5_000); });
    expect(ons.screenshot).toHaveBeenCalledOnce();
    expect(fetchMock).toHaveBeenCalledWith("/runtime/launches/preview-ons/review-screenshot", expect.objectContaining({
      method: "POST", body: expect.any(Blob),
    }));

    view.unmount();
    expect(ons.exit).toHaveBeenCalledOnce();
  });

  it("mounts KiriKiri, captures its canvas, and exits the runtime", async () => {
    vi.useFakeTimers();
    kirikiri.mount.mockResolvedValue(undefined);
    kirikiri.screenshot.mockResolvedValue(new Blob(["kirikiri-png"], { type: "image/png" }));
    kirikiri.exit.mockResolvedValue(undefined);
    kirikiri.create.mockReturnValue({
      mount: kirikiri.mount, screenshot: kirikiri.screenshot,
      exit: kirikiri.exit, subscribe: kirikiri.subscribe,
    });
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).endsWith("/config")) {
        return Promise.resolve(new Response(JSON.stringify({
          runtimeFamily: "KIRIKIRI", sessionId: "preview-kirikiri", gameTitle: "KAG fixture",
          adapter: {
            adapterKind: "KIRIKIRI2_WEB", adapterId: "kirikiri2-web",
            runtimeBaseUrl: "/runtime/retrom-runtime/v0.7.4/",
            projectIndexUrl: `/runtime/content/project/${"b".repeat(64)}/index.json`,
            startupXp3Path: null, checkpointSlot: 1999,
          },
          reviewPreview: { importItemId: "item-kirikiri", captureAllowed: true, captureAfterMs: 5000 },
        }), { status: 200, headers: { "Content-Type": "application/json" } }));
      }
      if (String(input).endsWith("/review-screenshot") && init?.method === "POST") {
        return Promise.resolve(new Response(JSON.stringify({ screenshotId: "shot-kirikiri" }), {
          status: 201, headers: { "Content-Type": "application/json" },
        }));
      }
      throw new Error(`unexpected fetch ${String(input)}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const view = render(<ReviewPreviewPlayer previewId="preview-kirikiri" />);
    await act(async () => {await Promise.resolve(); await Promise.resolve(); await Promise.resolve();});

    expect(kirikiri.create).toHaveBeenCalledOnce();
    expect(kirikiri.mount).toHaveBeenCalledOnce();
    await act(async () => {await vi.advanceTimersByTimeAsync(5_000);});
    expect(kirikiri.screenshot).toHaveBeenCalledOnce();
    expect(fetchMock).toHaveBeenCalledWith(
      "/runtime/launches/preview-kirikiri/review-screenshot",
      expect.objectContaining({ method: "POST", body: expect.any(Blob) }),
    );

    view.unmount();
    expect(kirikiri.exit).toHaveBeenCalledOnce();
  });
});
