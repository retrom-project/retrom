"use client";

import { useEffect, useRef, useState } from "react";
import {
  createRuntime,
  type GameRuntime,
  type KirikiriRuntimeConfig,
  type OnsRuntimeConfig,
  type ButterscotchRuntimeConfig,
  type TyranoScriptRuntimeConfig,
} from "@xxxsen/retrom-runtime";
import { captureReviewScreenshot, mountEmulatorJS, type EmulatorInstance, type PlayerConfig } from "@/features/player/adapters/ejs-4.2.3-v2";
import { installCanvasContain } from "@/features/player/canvas-fit";

type ReviewPreview = { importItemId: string; captureAllowed: boolean; captureAfterMs: 5000 };
type EmulatorReviewConfig = PlayerConfig & {
  runtimeFamily: "EMULATORJS";
  reviewPreview: ReviewPreview;
};
type ONSReviewConfig = OnsRuntimeConfig & {
  runtimeFamily: "ONS";
  gameTitle: string;
  reviewPreview: ReviewPreview;
};
type KiriKiriReviewConfig = KirikiriRuntimeConfig & {
  runtimeFamily: "KIRIKIRI";
  gameTitle: string;
  reviewPreview: ReviewPreview;
};
type ButterscotchReviewConfig = ButterscotchRuntimeConfig & {
  runtimeFamily: "BUTTERSCOTCH";
  gameTitle: string;
  reviewPreview: ReviewPreview;
};
type TyranoScriptReviewConfig = TyranoScriptRuntimeConfig & {
  runtimeFamily: "TYRANOSCRIPT";
  gameTitle: string;
  reviewPreview: ReviewPreview;
};
type ReviewPlayerConfig = EmulatorReviewConfig | ONSReviewConfig | KiriKiriReviewConfig |
  ButterscotchReviewConfig | TyranoScriptReviewConfig;

type ReviewRuntime = {
  screenshot: () => Promise<Blob>;
  exit: () => Promise<void>;
};

type ReviewMount = {
  runtime: ReviewRuntime;
  cleanup: () => void;
  emulator?: EmulatorInstance;
};

type ReviewMountOptions = {
  frame: HTMLIFrameElement;
  signal: AbortSignal;
  onError: (reason: unknown) => void;
  onExitRequested: () => void;
  onStart: (mount: ReviewMount) => void;
};

async function mountReviewRuntime(
  config: ReviewPlayerConfig,
  target: HTMLElement,
  frameWindow: Window,
  options: ReviewMountOptions,
): Promise<void> {
  if (config.runtimeFamily === "ONS") {
    const runtimeConfig: OnsRuntimeConfig = { sessionId: config.sessionId, adapter: config.adapter };
    const runtime: GameRuntime = createRuntime(runtimeConfig, {
      frameWindow, restorePayload: null, signal: options.signal,
    });
    const unsubscribe = runtime.subscribe((event) => {
      if (event.type === "FATAL_ERROR") {options.onError(event.code);}
      if (event.type === "EXIT_REQUESTED") {options.onExitRequested();}
    });
    try {
      await runtime.mount(target);
    } catch (error) {
      unsubscribe();
      await runtime.exit();
      throw error;
    }
    options.onStart({
      runtime,
      cleanup: () => { unsubscribe(); void runtime.exit(); },
    });
    return;
  }
  if (config.runtimeFamily === "KIRIKIRI") {
    const runtimeConfig: KirikiriRuntimeConfig = { sessionId: config.sessionId, adapter: config.adapter };
    const runtime: GameRuntime = createRuntime(runtimeConfig, {
      frameWindow, restorePayload: null, signal: options.signal,
    });
    const unsubscribe = runtime.subscribe((event) => {
      if (event.type === "FATAL_ERROR") {options.onError(event.code);}
      if (event.type === "EXIT_REQUESTED") {options.onExitRequested();}
    });
    try {
      await runtime.mount(target);
    } catch (error) {
      unsubscribe();
      await runtime.exit();
      throw error;
    }
    options.onStart({runtime, cleanup: () => {unsubscribe(); void runtime.exit();}});
    return;
  }
  if (config.runtimeFamily === "BUTTERSCOTCH") {
    const runtimeConfig: ButterscotchRuntimeConfig = {
      sessionId: config.sessionId, contentDigest: config.contentDigest, adapter: config.adapter,
    };
    const runtime: GameRuntime = createRuntime(runtimeConfig, {
      frameWindow, restorePayload: null, signal: options.signal,
    });
    const unsubscribe = runtime.subscribe((event) => {
      if (event.type === "FATAL_ERROR") {options.onError(event.code);}
      if (event.type === "EXIT_REQUESTED") {options.onExitRequested();}
    });
    try {await runtime.mount(target);} catch (error) {
      unsubscribe(); await runtime.exit(); throw error;
    }
    options.onStart({runtime, cleanup: () => {unsubscribe(); void runtime.exit();}});
    return;
  }
  if (config.runtimeFamily === "TYRANOSCRIPT") {
    const runtimeConfig: TyranoScriptRuntimeConfig = {
      sessionId: config.sessionId, contentDigest: config.contentDigest, adapter: config.adapter,
    };
    const runtime: GameRuntime = createRuntime(runtimeConfig, {
      frame: options.frame, frameWindow, restorePayload: null, signal: options.signal,
    });
    const unsubscribe = runtime.subscribe((event) => {
      if (event.type === "FATAL_ERROR") {options.onError(event.code);}
      if (event.type === "EXIT_REQUESTED") {options.onExitRequested();}
    });
    try {await runtime.mount(target);} catch (error) {
      unsubscribe(); await runtime.exit(); throw error;
    }
    options.onStart({runtime, cleanup: () => {unsubscribe(); void runtime.exit();}});
    return;
  }
  let emulator: EmulatorInstance | undefined;
  let cleanup: () => void = () => undefined;
  cleanup = mountEmulatorJS(config, target, {
    onReady: (value) => { emulator = value; value.on("exit", options.onExitRequested); },
    onGameStart: () => {
      if (!emulator) {options.onError("模拟器核心尚未就绪"); return false;}
      options.onStart({
        emulator,
        runtime: {
          screenshot: async () => (await captureReviewScreenshot(emulator as EmulatorInstance)).screenshot,
          exit: async () => { cleanup(); },
        },
        cleanup,
      });
    },
  }, frameWindow);
}

type PreviewState = "loading" | "running" | "capturing" | "captured" | "exited" | "error";
const previewStartupTimeoutMs = 30_000;

function stateCopy(state: PreviewState) {
  if (state === "loading") {return "正在冻结审核来源并加载核心…";}
  if (state === "capturing") {return "游戏已运行 5 秒，正在保存审核截图…";}
  if (state === "captured") {return "第 5 秒运行截图已保存；可以继续试玩。";}
  if (state === "exited") {return "游戏已从自身菜单退出，正在关闭子窗体。";}
  if (state === "running") {return "游戏已启动，将在第 5 秒自动保存截图。";}
  return "审核预览启动失败。";
}

export function ReviewPreviewPlayer({ previewId }: { previewId: string }) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const emulatorRef = useRef<EmulatorInstance | undefined>(undefined);
  const runtimeRef = useRef<ReviewRuntime | undefined>(undefined);
  const captureTimerRef = useRef<number | null>(null);
  const [state, setState] = useState<PreviewState>("loading");
  const [title, setTitle] = useState("审核游戏预览");
  const [detail, setDetail] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    let cleanup: (() => void) | undefined;
    let canvasContain: ReturnType<typeof installCanvasContain> | undefined;
    let frameWindow: Window | undefined;
    let startupTimer: number | undefined;
    let gameStarted = false;
    let startupFailed = false;

    function failStartup(reason: unknown) {
      if (controller.signal.aborted || gameStarted || startupFailed) {return;}
      startupFailed = true;
      if (startupTimer !== undefined) {window.clearTimeout(startupTimer);}
      const message = reason instanceof Error ? reason.message : typeof reason === "string" ? reason : "模拟器核心启动失败";
      setDetail(message);
      setState("error");
    }

    const onRuntimeError = (event: ErrorEvent) => {
      failStartup(event.error instanceof Error ? event.error : event.message || "模拟器核心启动失败");
    };
    const onRuntimeRejection = (event: PromiseRejectionEvent) => {
      failStartup(event.reason);
    };

    async function uploadCapture(config: ReviewPlayerConfig) {
      const runtime = runtimeRef.current;
      if (!runtime) {throw new Error("预览核心尚未就绪，无法截图");}
      setState("capturing");
      const screenshot = await runtime.screenshot();
      const response = await fetch(`/runtime/launches/${previewId}/review-screenshot`, {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": screenshot.type || "application/octet-stream" },
        body: screenshot,
      });
      if (!response.ok) {
        const payload = await response.json().catch(() => null) as { error?: { message?: string } } | null;
        throw new Error(payload?.error?.message ?? "第 5 秒运行截图保存失败");
      }
      setState("captured");
      window.opener?.postMessage({
        type: "retrom-review-screenshot",
        importItemId: config.reviewPreview.importItemId,
        previewId,
      }, window.location.origin);
    }

    async function bootstrap() {
      try {
        const response = await fetch(`/runtime/launches/${previewId}/config`, {
          credentials: "same-origin", cache: "no-store", signal: controller.signal,
        });
        if (!response.ok) {throw new Error("审核预览会话已失效，请回到审核页重新运行");}
        const config = await response.json() as ReviewPlayerConfig;
        if (!config.reviewPreview || config.reviewPreview.captureAfterMs !== 5000) {throw new Error("审核预览配置无效");}
        setTitle(config.gameTitle || "审核游戏预览");
        const frame = frameRef.current;
        const mountedFrameWindow = frame?.contentWindow ?? undefined;
        frameWindow = mountedFrameWindow;
        const frameDocument = frame?.contentDocument;
        if (!frame || !mountedFrameWindow || !frameDocument) {throw new Error("无法创建游戏子窗体");}
        mountedFrameWindow.addEventListener("error", onRuntimeError);
        mountedFrameWindow.addEventListener("unhandledrejection", onRuntimeRejection);
        frameDocument.documentElement.lang = "zh-CN";
        const style = frameDocument.createElement("style");
        style.textContent = `html,body,#game,#retrom-emulator,.ejs_parent,.ejs_game,.ejs_canvas_parent{width:100%!important;height:100%!important;margin:0!important;overflow:hidden;background:#05060a}.ejs_canvas_parent{display:grid!important;place-items:center!important}canvas{display:block;max-width:none!important;max-height:none!important;margin:auto!important}`;
        frameDocument.head.append(style);
        const target = frameDocument.createElement("div");
        target.id = "game";
        frameDocument.body.append(target);
        canvasContain = installCanvasContain(frameDocument, () => emulatorRef.current?.gameManager?.getVideoDimensions?.("aspect"));
        startupTimer = window.setTimeout(() => failStartup("模拟器核心启动超时，请关闭子窗体后重试"), previewStartupTimeoutMs);
        await mountReviewRuntime(config, target, mountedFrameWindow, {
          frame,
          signal: controller.signal,
          onError: failStartup,
          onExitRequested: () => {
            if (captureTimerRef.current !== null) {window.clearTimeout(captureTimerRef.current);}
            setState("exited");
            cleanup?.();
            window.close();
          },
          onStart: (mount) => {
            if (startupFailed || controller.signal.aborted) {mount.cleanup(); return;}
            gameStarted = true;
            runtimeRef.current = mount.runtime;
            emulatorRef.current = mount.emulator;
            cleanup = mount.cleanup;
            if (startupTimer !== undefined) {window.clearTimeout(startupTimer);}
            setState("running");
            window.requestAnimationFrame(() => canvasContain?.refresh());
            captureTimerRef.current = window.setTimeout(() => {
              void uploadCapture(config).catch((error: unknown) => {
                setDetail(error instanceof Error ? error.message : "第 5 秒运行截图保存失败");
                setState("error");
              });
            }, config.reviewPreview.captureAfterMs);
          },
        });
      } catch (error) {
        if (controller.signal.aborted) {return;}
        failStartup(error);
      }
    }
    void bootstrap();
    return () => {
      controller.abort();
      if (captureTimerRef.current !== null) {window.clearTimeout(captureTimerRef.current);}
      if (startupTimer !== undefined) {window.clearTimeout(startupTimer);}
      frameWindow?.removeEventListener("error", onRuntimeError);
      frameWindow?.removeEventListener("unhandledrejection", onRuntimeRejection);
      cleanup?.();
      runtimeRef.current = undefined;
      canvasContain?.cleanup();
    };
  }, [previewId]);

  return <main className="review-preview-player">
    <header className={`review-preview-status is-${state}`}>
      <div><strong>{title}</strong><span>{stateCopy(state)}</span>{detail ? <small role="alert">{detail}</small> : null}</div>
      <button type="button" onClick={() => window.close()}>关闭子窗体</button>
    </header>
    <iframe ref={frameRef} src="about:blank" title={`${title} 运行画面`} className="review-preview-frame" />
  </main>;
}
