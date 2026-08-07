"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { newUuid } from "@/lib/crypto";

type LaunchResponse = { launchId: string; playUrl: string };
type PendingResponse = { status: "VALIDATION_PENDING"; jobId: string; retryAfterMs: number };

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
      if (error) reject(error); else resolve();
    };
    source.addEventListener("snapshot", (event) => {
      const snapshot = JSON.parse((event as MessageEvent<string>).data) as { state?: string; errorCode?: string | null };
      if (snapshot.state === "SUCCEEDED") finish();
      if (snapshot.state === "FAILED" || snapshot.state === "CANCELLED") finish(new Error(snapshot.errorCode ?? "核心验证失败"));
    });
    source.addEventListener("succeeded", () => finish());
    source.addEventListener("failed", (event) => {
      const details = JSON.parse((event as MessageEvent<string>).data) as { code?: string };
      finish(new Error(details.code ?? "核心验证失败"));
    });
    source.addEventListener("cancelled", () => finish(new Error("核心验证已取消")));
  });
}

export function LaunchButton({ gameId, coreId = null, saveStateId = null, dosEntry = null, returnTo = `/games/${gameId}`, disabled = false, label = "开始游戏" }: { gameId: string; coreId?: string | null; saveStateId?: string | null; dosEntry?: string | null; returnTo?: string; disabled?: boolean; label?: string }) {
  const router = useRouter();
  const [state, setState] = useState<"idle" | "starting" | "blocked">("idle");
  const [message, setMessage] = useState("");

  async function launch() {
    setState("starting");
    // Fullscreen must be requested directly from the trusted click; waiting for
    // the API response would lose browser user activation.
    void document.documentElement.requestFullscreen().catch(() => undefined);
    try {
      const body = JSON.stringify({
        gameId,
        coreId,
        saveStateId,
        dosEntry,
        returnTo,
        clientCapabilities: {
          secureContext: window.isSecureContext,
          crossOriginIsolated: window.crossOriginIsolated,
          sharedArrayBuffer: typeof SharedArrayBuffer !== "undefined"
        }
      });
      for (let attempt = 0; attempt < 2; attempt += 1) {
        const response = await fetch("/api/v1/launches", {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json", "Idempotency-Key": newUuid() },
          body
        });
        if (!response.ok) {
          const result = await response.json() as { error?: { message?: string } };
          throw new Error(result.error?.message ?? "当前配置无法启动");
        }
        if (response.status === 202) {
          const pending = await response.json() as PendingResponse;
          if (pending.status !== "VALIDATION_PENDING" || !pending.jobId) throw new Error("核心验证响应无效");
          await waitForValidation(pending.jobId);
          continue;
        }
        const result = await response.json() as LaunchResponse;
        router.replace(result.playUrl);
        return;
      }
      throw new Error("核心验证完成后仍无法启动");
    } catch (error) {
      if (document.fullscreenElement) await document.exitFullscreen().catch(() => undefined);
      setMessage(error instanceof Error ? error.message : "启动失败");
      setState("blocked");
    }
  }

  const biosBlocked = /BIOS|固件/.test(message);
  return <><button className="button" disabled={disabled || state === "starting"} onClick={() => void launch()}>{state === "starting" ? "正在准备运行环境…" : label}</button>{state === "blocked" ? <p role="alert" className="status bad">{message}{biosBlocked ? <> <Link href="/admin/bios?scope=REQUIRED_BY_LIBRARY">前往 BIOS 管理</Link></> : null}</p> : null}</>;
}
