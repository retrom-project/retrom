"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { useToast } from "@/components/toast-provider";
import { newUuid } from "@/lib/crypto";
import { writeHeaders } from "@/lib/api/client";
import { replaceWithPlayerDocument } from "@/lib/player-document-navigation";
import { requestFullscreenAndLandscape, unlockLandscape } from "./orientation";

import {rememberPlayerGame} from "./gamepad-cursor-preference";

type LaunchResponse = { launchId: string; playUrl: string };
type PendingResponse = { status: "VALIDATION_PENDING"; jobId: string; retryAfterMs: number };
type ClientCapabilities = { secureContext: boolean; crossOriginIsolated: boolean; sharedArrayBuffer: boolean };

function lacksRequiredThreads(required: boolean, capabilities: ClientCapabilities) {
  return required && !(capabilities.secureContext && capabilities.crossOriginIsolated && capabilities.sharedArrayBuffer);
}

function waitForValidation(jobId: string) {
  return new Promise<void>((resolve, reject) => {
    const source = new EventSource(`/api/v1/admin/jobs/${encodeURIComponent(jobId)}/events`, { withCredentials: true });
    const timeout = window.setTimeout(() => {
      source.close();
      reject(new Error("核心验证超时，请在任务页查看详情"));
    }, 120_000);
    const finish = (error?: Error) => {
      window.clearTimeout(timeout);
      source.close();
      if (error) {reject(error);} else {resolve();}
    };
    source.addEventListener("snapshot", (event) => {
      const snapshot = JSON.parse((event as MessageEvent<string>).data) as { state?: string; errorCode?: string | null };
      if (snapshot.state === "SUCCEEDED") {finish();}
      if (snapshot.state === "FAILED" || snapshot.state === "CANCELLED") {finish(new Error(snapshot.errorCode ?? "核心验证失败"));}
    });
    source.addEventListener("succeeded", () => finish());
    source.addEventListener("failed", (event) => {
      const details = JSON.parse((event as MessageEvent<string>).data) as { code?: string };
      finish(new Error(details.code ?? "核心验证失败"));
    });
    source.addEventListener("cancelled", () => finish(new Error("核心验证已取消")));
  });
}

export function LaunchButton({ gameId, coreId = null, saveStateId = null, dosEntry = null, returnTo = `/games/${gameId}`, requiresThreads = false, disabled = false, label = "开始游戏", onLaunchCreated, secondary = false }: { gameId: string; coreId?: string | null; saveStateId?: string | null; dosEntry?: string | null; returnTo?: string; requiresThreads?: boolean; disabled?: boolean; label?: string; onLaunchCreated?: () => void; secondary?: boolean }) {
  const router = useRouter();
  const { notify, clear } = useToast();
  const [starting, setStarting] = useState(false);

  async function launch() {
    clear();
    setStarting(true);
    const clientCapabilities = {
      secureContext: window.isSecureContext,
      crossOriginIsolated: window.crossOriginIsolated,
      sharedArrayBuffer: typeof SharedArrayBuffer !== "undefined"
    };
    if (lacksRequiredThreads(requiresThreads, clientCapabilities)) {
      notify({ tone: "bad", message: "当前浏览器环境不提供该运行方式所需的线程能力；远程明文 HTTP 无法提供 SharedArrayBuffer。" });
      setStarting(false);
      return;
    }
    // Fullscreen must be requested directly from the trusted click; waiting for
    // the API response would lose browser user activation.
    void requestFullscreenAndLandscape();
    try {
      const body = JSON.stringify({
        gameId,
        coreId,
        saveStateId,
        dosEntry,
        returnTo,
        clientCapabilities
      });
      for (let attempt = 0; attempt < 2; attempt += 1) {
        const response = await fetch("/api/v1/launches", {
          method: "POST",
          credentials: "same-origin",
          headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }),
          body
        });
        if (!response.ok) {
          const result = await response.json() as { error?: { message?: string } };
          throw new Error(result.error?.message ?? "当前配置无法启动");
        }
        if (response.status === 202) {
          const pending = await response.json() as PendingResponse;
          if (pending.status !== "VALIDATION_PENDING" || !pending.jobId) {throw new Error("核心验证响应无效");}
          await waitForValidation(pending.jobId);
          continue;
        }
        const result = await response.json() as LaunchResponse;
        rememberPlayerGame(result.launchId, gameId);
        onLaunchCreated?.();
        replaceWithPlayerDocument(result.playUrl, router.replace);
        return;
      }
      throw new Error("核心验证完成后仍无法启动");
    } catch (error) {
      unlockLandscape();
      if (document.fullscreenElement) {await document.exitFullscreen().catch(() => undefined);}
      const message = error instanceof Error ? error.message : "启动失败";
      notify({ tone: "bad", message, action: /BIOS|固件/.test(message) ? { href: "/admin/bios?scope=REQUIRED_BY_LIBRARY", label: "前往 BIOS 管理" } : undefined });
      setStarting(false);
    }
  }

  return <button className={secondary ? "button secondary" : "button"} disabled={disabled || starting} onClick={() => void launch()}>{starting ? "正在准备运行环境…" : label}</button>;
}
