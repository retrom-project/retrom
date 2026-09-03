"use client";

import {useEffect, useRef, useState} from "react";
import type {PlayerRuntimeV1} from "@/features/player/runtime/contract";
import {parseLaunchEnvelopeJSON} from "@/features/player/runtime/envelope";
import {mountProviderRuntime, type RuntimeController} from "@/features/player/runtime/runtime-controller";

type PreviewState = "loading" | "running" | "capturing" | "captured" | "exited" | "error";
const captureAfterMs = 5_000;

function stateCopy(state: PreviewState) {
  if (state === "loading") {return "正在冻结审核来源并加载 Provider…";}
  if (state === "capturing") {return "游戏已运行 5 秒，正在保存审核截图…";}
  if (state === "captured") {return "第 5 秒运行截图已保存；可以继续试玩。";}
  if (state === "exited") {return "游戏已从自身菜单退出，正在关闭子窗体。";}
  if (state === "running") {return "游戏已启动，将在第 5 秒自动保存截图。";}
  return "审核预览启动失败。";
}

export function ReviewPreviewPlayer({previewId}: {previewId: string}) {
  const targetRef = useRef<HTMLDivElement>(null);
  const runtimeRef = useRef<PlayerRuntimeV1 | null>(null);
  const controllerRef = useRef<RuntimeController | null>(null);
  const captureTimerRef = useRef<number | null>(null);
  const [state, setState] = useState<PreviewState>("loading");
  const [title, setTitle] = useState("审核游戏预览");
  const [detail, setDetail] = useState("");

  useEffect(() => {
    const abort = new AbortController();
    let terminal = false;

    const fail = (reason: unknown) => {
      if (abort.signal.aborted || terminal) {return;}
      setDetail(reason instanceof Error ? reason.message : typeof reason === "string" ? reason : "Provider 启动失败");
      setState("error");
    };

    const exitRequested = () => {
      if (terminal) {return;}
      terminal = true;
      if (captureTimerRef.current !== null) {window.clearTimeout(captureTimerRef.current);}
      setState("exited");
      window.close();
    };

    async function uploadCapture() {
      const runtime = runtimeRef.current;
      if (!runtime?.getCapabilities().screenshot) {throw new Error("当前 Provider 不支持审核截图");}
      setState("capturing");
      const screenshot = await runtime.screenshot();
      const response = await fetch(`/runtime/launches/${previewId}/review-screenshot`, {
        method: "POST", credentials: "same-origin",
        headers: {"Content-Type": screenshot.type || "application/octet-stream"}, body: screenshot,
      });
      const payload = await response.json().catch(() => null) as {
        importItemId?: string; error?: {message?: string};
      } | null;
      if (!response.ok) {throw new Error(payload?.error?.message ?? "第 5 秒运行截图保存失败");}
      if (!payload?.importItemId) {throw new Error("审核截图响应无效");}
      setState("captured");
      window.opener?.postMessage({
        type: "retrom-review-screenshot", importItemId: payload.importItemId, previewId,
      }, window.location.origin);
    }

    async function bootstrap() {
      const response = await fetch(`/runtime/launches/${previewId}/config`, {
        credentials: "same-origin", cache: "no-store", signal: abort.signal,
      });
      if (!response.ok) {throw new Error("审核预览会话已失效，请回到审核页重新运行");}
      const envelope = parseLaunchEnvelopeJSON(await response.text());
      if (envelope.session.purpose !== "REVIEW_PREVIEW" || envelope.session.mode !== "SINGLE") {
        throw new Error("审核预览启动契约无效");
      }
      const target = targetRef.current;
      if (!target) {throw new Error("无法创建 Provider 挂载点");}
      setTitle(envelope.session.title);
      const controller = await mountProviderRuntime(envelope, target, {
        signal: abort.signal,
        onExitRequested: exitRequested,
        onFatalError: fail,
      });
      if (abort.signal.aborted) {await controller.exit(); return;}
      controllerRef.current = controller;
      runtimeRef.current = controller.runtime;
      if (!controller.runtime.getCapabilities().screenshot) {
        throw new Error("当前 Provider 不支持审核截图");
      }
      setState("running");
      captureTimerRef.current = window.setTimeout(() => {
        void uploadCapture().catch(fail);
      }, captureAfterMs);
    }

    void bootstrap().catch(fail);
    return () => {
      abort.abort();
      if (captureTimerRef.current !== null) {window.clearTimeout(captureTimerRef.current);}
      captureTimerRef.current = null;
      runtimeRef.current = null;
      const controller = controllerRef.current;
      controllerRef.current = null;
      void controller?.exit().catch(() => undefined);
    };
  }, [previewId]);

  return <main className="review-preview-player">
    <header className={`review-preview-status is-${state}`}>
      <div><strong>{title}</strong><span>{stateCopy(state)}</span>{detail ? <small role="alert">{detail}</small> : null}</div>
      <button type="button" onClick={() => window.close()}>关闭子窗体</button>
    </header>
    <div ref={targetRef} title={`${title} 运行画面`} className="review-preview-frame" />
  </main>;
}
