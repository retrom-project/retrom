"use client";

import { useEffect, useRef, useState } from "react";
import { captureManualScreenshot, mountEmulatorJS, type EmulatorInstance, type PlayerConfig } from "@/features/player/adapters/ejs-4.2.3-v2";
import { installCanvasContain } from "@/features/player/canvas-fit";

type ReviewPlayerConfig = PlayerConfig & {
  reviewPreview: { importItemId: string; captureAllowed: boolean; captureAfterMs: 5000 };
};

type PreviewState = "loading" | "running" | "capturing" | "captured" | "error";

function stateCopy(state: PreviewState) {
  if (state === "loading") return "正在冻结审核来源并加载核心…";
  if (state === "capturing") return "游戏已运行 5 秒，正在保存审核截图…";
  if (state === "captured") return "第 5 秒运行截图已保存；可以继续试玩。";
  if (state === "running") return "游戏已启动，将在第 5 秒自动保存截图。";
  return "审核预览启动失败。";
}

export function ReviewPreviewPlayer({ previewId }: { previewId: string }) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const emulatorRef = useRef<EmulatorInstance | undefined>(undefined);
  const captureTimerRef = useRef<number | null>(null);
  const [state, setState] = useState<PreviewState>("loading");
  const [title, setTitle] = useState("审核游戏预览");
  const [detail, setDetail] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    let cleanup: (() => void) | undefined;
    let canvasContain: ReturnType<typeof installCanvasContain> | undefined;

    async function uploadCapture(config: ReviewPlayerConfig) {
      const emulator = emulatorRef.current;
      if (!emulator) throw new Error("预览核心尚未就绪，无法截图");
      setState("capturing");
      const capture = await captureManualScreenshot(emulator);
      const response = await fetch(`/runtime/launches/${previewId}/review-screenshot`, {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "image/png" },
        body: capture.screenshot,
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
        if (!response.ok) throw new Error("审核预览会话已失效，请回到审核页重新运行");
        const config = await response.json() as ReviewPlayerConfig;
        if (!config.reviewPreview || config.reviewPreview.captureAfterMs !== 5000) throw new Error("审核预览配置无效");
        setTitle(config.gameTitle || "审核游戏预览");
        const frame = frameRef.current;
        const frameWindow = frame?.contentWindow;
        const frameDocument = frame?.contentDocument;
        if (!frame || !frameWindow || !frameDocument) throw new Error("无法创建游戏子窗体");
        frameDocument.documentElement.lang = "zh-CN";
        const style = frameDocument.createElement("style");
        style.textContent = `html,body,#game,#retrom-emulator,.ejs_parent,.ejs_game,.ejs_canvas_parent{width:100%!important;height:100%!important;margin:0!important;overflow:hidden;background:#05060a}.ejs_canvas_parent{display:grid!important;place-items:center!important}canvas{display:block;max-width:none!important;max-height:none!important;margin:auto!important}`;
        frameDocument.head.append(style);
        const target = frameDocument.createElement("div");
        target.id = "game";
        frameDocument.body.append(target);
        canvasContain = installCanvasContain(frameDocument, () => emulatorRef.current?.gameManager?.getVideoDimensions?.("aspect"));
        cleanup = mountEmulatorJS(config, target, {
          onReady: (emulator) => { emulatorRef.current = emulator; },
          onGameStart: () => {
            setState("running");
            frameWindow.requestAnimationFrame(() => canvasContain?.refresh());
            captureTimerRef.current = window.setTimeout(() => {
              void uploadCapture(config).catch((error: unknown) => {
                setDetail(error instanceof Error ? error.message : "第 5 秒运行截图保存失败");
                setState("error");
              });
            }, config.reviewPreview.captureAfterMs);
          },
        }, frameWindow);
      } catch (error) {
        if (controller.signal.aborted) return;
        setDetail(error instanceof Error ? error.message : "审核预览启动失败");
        setState("error");
      }
    }
    void bootstrap();
    return () => {
      controller.abort();
      if (captureTimerRef.current !== null) window.clearTimeout(captureTimerRef.current);
      cleanup?.();
      canvasContain?.cleanup();
    };
  }, [previewId]);

  return <main className="review-preview-player">
    <header className={`review-preview-status is-${state}`}>
      <div><strong>{title}</strong><span>{stateCopy(state)}</span>{detail ? <small role="alert">{detail}</small> : null}</div>
      <button type="button" onClick={() => window.close()}>关闭子窗体</button>
    </header>
    <iframe ref={frameRef} title={`${title} 运行画面`} className="review-preview-frame" />
  </main>;
}
