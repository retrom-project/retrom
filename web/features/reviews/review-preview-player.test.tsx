import { act, cleanup, render, screen } from "@testing-library/react";
import type { GameRuntimeEvent } from "@xxxsen/retrom-runtime";
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
  subscribe: vi.fn((listener: (event: GameRuntimeEvent) => void) => {void listener; return vi.fn();}),
}));
const kirikiri = vi.hoisted(() => ({
  create: vi.fn(),
  mount: vi.fn(),
  screenshot: vi.fn(),
  exit: vi.fn(),
  subscribe: vi.fn((listener: (event: GameRuntimeEvent) => void) => {void listener; return vi.fn();}),
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

async function verifyWASM4Preview() {
  vi.useFakeTimers();
  kirikiri.mount.mockResolvedValue(undefined);
  kirikiri.screenshot.mockResolvedValue(new Blob(["wasm4-png"], {type: "image/png"}));
  kirikiri.exit.mockResolvedValue(undefined);
  kirikiri.create.mockReturnValue({
    mount: kirikiri.mount, screenshot: kirikiri.screenshot,
    exit: kirikiri.exit, subscribe: kirikiri.subscribe,
  });
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    if (String(input).endsWith("/config")) {
      return Promise.resolve(new Response(JSON.stringify({
        runtimeFamily: "WASM4", sessionId: "preview-wasm4", gameTitle: "Pong",
        contentDigest: "a".repeat(64), cartSizeBytes: 6818,
        adapter: {
          adapterKind: "WASM4_WEB", adapterId: "wasm4-web",
          runtimeBaseUrl: "/runtime/retrom-runtime/v0.11.1/",
          cartUrl: `/runtime/content/game/${"b".repeat(64)}/pong.wasm`,
        },
        reviewPreview: {importItemId: "item-wasm4", captureAllowed: true, captureAfterMs: 5000},
      }), {status: 200, headers: {"Content-Type": "application/json"}}));
    }
    if (String(input).endsWith("/review-screenshot") && init?.method === "POST") {
      return Promise.resolve(new Response(JSON.stringify({screenshotId: "shot-wasm4"}), {
        status: 201, headers: {"Content-Type": "application/json"},
      }));
    }
    throw new Error(`unexpected fetch ${String(input)}`);
  });
  vi.stubGlobal("fetch", fetchMock);

  const view = render(<ReviewPreviewPlayer previewId="preview-wasm4" />);
  await act(async () => {await Promise.resolve(); await Promise.resolve(); await Promise.resolve();});

  expect(kirikiri.create).toHaveBeenCalledWith(expect.objectContaining({
    sessionId: "preview-wasm4", cartSizeBytes: 6818,
    adapter: expect.objectContaining({adapterKind: "WASM4_WEB"}),
  }));
  expect(kirikiri.mount).toHaveBeenCalledOnce();
  await act(async () => {await vi.advanceTimersByTimeAsync(5_000);});
  expect(fetchMock).toHaveBeenCalledWith(
    "/runtime/launches/preview-wasm4/review-screenshot",
    expect.objectContaining({method: "POST", body: expect.any(Blob)}),
  );

  view.unmount();
  expect(kirikiri.exit).toHaveBeenCalledOnce();
}

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

  it("closes an ONS preview without capturing after the game exits itself", async () => {
    vi.useFakeTimers();
    let reportEvent: ((event: GameRuntimeEvent) => void) | undefined;
    const unsubscribe = vi.fn();
    ons.mount.mockResolvedValue(undefined);
    ons.exit.mockResolvedValue(undefined);
    ons.subscribe.mockImplementation((listener) => {
      reportEvent = listener;
      return unsubscribe;
    });
    ons.create.mockReturnValue({
      mount: ons.mount, screenshot: ons.screenshot, exit: ons.exit, subscribe: ons.subscribe,
    });
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response(JSON.stringify({
      runtimeFamily: "ONS", sessionId: "preview-ons-exit", gameTitle: "ONS exit fixture",
      adapter: {
        adapterKind: "ONS_YURI_WEB", adapterId: "ons-yuri-web",
        runtimeBaseUrl: "/runtime/retrom-runtime/v0.7.5/",
        projectIndexUrl: `/runtime/content/project/${"c".repeat(64)}/index.json`,
        scriptEncoding: "utf8", checkpointSlot: 999,
      },
      reviewPreview: { importItemId: "item-ons-exit", captureAllowed: true, captureAfterMs: 5000 },
    }), { status: 200, headers: { "Content-Type": "application/json" } }))));
    const close = vi.spyOn(window, "close").mockImplementation(() => undefined);

    render(<ReviewPreviewPlayer previewId="preview-ons-exit" />);
    await act(async () => {await Promise.resolve(); await Promise.resolve(); await Promise.resolve();});
    act(() => reportEvent?.({ type: "EXIT_REQUESTED" }));

    expect(screen.getByText("游戏已从自身菜单退出，正在关闭子窗体。")).toBeVisible();
    expect(unsubscribe).toHaveBeenCalledOnce();
    expect(ons.exit).toHaveBeenCalledOnce();
    expect(close).toHaveBeenCalledOnce();
    await act(async () => {await vi.advanceTimersByTimeAsync(5_000);});
    expect(ons.screenshot).not.toHaveBeenCalled();
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
            runtimeBaseUrl: "/runtime/retrom-runtime/v0.7.5/",
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

  it("mounts a bounded WASM-4 cart and uploads its review screenshot", verifyWASM4Preview);

  it("mounts isolated TyranoScript and uploads its JPEG review screenshot", async () => {
    vi.useFakeTimers();
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((callback) => {
      callback(0);
      return 1;
    });
    kirikiri.mount.mockResolvedValue(undefined);
    kirikiri.screenshot.mockResolvedValue(new Blob(["tyrano-jpeg"], {type: "image/jpeg"}));
    kirikiri.exit.mockResolvedValue(undefined);
    kirikiri.create.mockReturnValue({
      mount: kirikiri.mount, screenshot: kirikiri.screenshot,
      exit: kirikiri.exit, subscribe: kirikiri.subscribe,
    });
    const origin = "https://preview-tyrano.rpg-runtime.example";
    const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      if (String(input).endsWith("/config")) {
        return Promise.resolve(new Response(JSON.stringify({
          runtimeFamily: "TYRANOSCRIPT", sessionId: "preview-tyrano", gameTitle: "Tyrano fixture",
          contentDigest: "a".repeat(64),
          adapter: {
            adapterKind: "TYRANOSCRIPT_WEB", adapterId: "tyranoscript-web",
            bootstrapTicket: "A".repeat(43), cleanupUrl: `${origin}/__retrom/tyranoscript/cleanup`,
            entryUrl: `${origin}/__retrom/tyranoscript/bootstrap`, uniqueOrigin: origin,
          },
          reviewPreview: {importItemId: "item-tyrano", captureAllowed: true, captureAfterMs: 5000},
        }), {status: 200, headers: {"Content-Type": "application/json"}}));
      }
      if (String(input).endsWith("/review-screenshot") && init?.method === "POST") {
        return Promise.resolve(new Response(JSON.stringify({screenshotId: "shot-tyrano"}), {
          status: 201, headers: {"Content-Type": "application/json"},
        }));
      }
      throw new Error(`unexpected fetch ${String(input)}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const view = render(<ReviewPreviewPlayer previewId="preview-tyrano" />);
    const frame = screen.getByTitle("审核游戏预览 运行画面") as HTMLIFrameElement;
    Object.defineProperty(frame.contentWindow, "requestAnimationFrame", {
      configurable: true,
      value: () => {throw new DOMException("Blocked a frame with origin", "SecurityError");},
    });
    await act(async () => {await Promise.resolve(); await Promise.resolve(); await Promise.resolve();});

    expect(kirikiri.create).toHaveBeenCalledOnce();
    expect(kirikiri.mount).toHaveBeenCalledOnce();
    await act(async () => {await vi.advanceTimersByTimeAsync(5_000);});
    expect(fetchMock).toHaveBeenCalledWith(
      "/runtime/launches/preview-tyrano/review-screenshot",
      expect.objectContaining({
        method: "POST", body: expect.any(Blob), headers: {"Content-Type": "image/jpeg"},
      }),
    );

    view.unmount();
    expect(kirikiri.exit).toHaveBeenCalledOnce();
  });
});
