import type { components } from "@/lib/api/generated/schema";
import { api, writeHeaders } from "@/lib/api/client";
import { newUuid } from "@/lib/crypto";

export type ImmersivePlatform = components["schemas"]["ImmersivePlatformSummary"];
export type ImmersivePlatformList = components["schemas"]["ImmersivePlatformList"];
export type ImmersiveGame = components["schemas"]["ImmersiveGameItem"];
export type ImmersiveGameList = components["schemas"]["ImmersiveGameList"];
type LaunchRequest = components["schemas"]["LaunchRequest"];

export class ImmersiveAPIError extends Error {
  constructor(public readonly status: number, message: string) {super(message);}
}

function errorMessage(error: unknown, fallback: string) {
  if (error && typeof error === "object" && "error" in error) {
    const envelope = error.error;
    if (envelope && typeof envelope === "object" && "message" in envelope && typeof envelope.message === "string") {
      return envelope.message;
    }
  }
  return fallback;
}

export async function fetchImmersivePlatforms(signal?: AbortSignal) {
  const { data, error, response } = await api.GET("/api/v1/immersive/platforms", { signal });
  if (!data) {throw new ImmersiveAPIError(response.status, errorMessage(error, "无法读取游戏平台"));}
  return data;
}

export async function fetchImmersiveGames(platformId: string, cursor: string | null, signal?: AbortSignal) {
  const { data, error, response } = await api.GET("/api/v1/immersive/platforms/{platformId}/games", {
    params: { path: { platformId }, query: { limit: 50, ...(cursor ? { cursor } : {}) } },
    signal,
  });
  if (!data) {throw new ImmersiveAPIError(response.status, errorMessage(error, "无法读取平台游戏"));}
  return data;
}

function waitForValidation(jobId: string) {
  return new Promise<void>((resolve, reject) => {
    const source = new EventSource(`/api/v1/admin/jobs/${encodeURIComponent(jobId)}/events`, { withCredentials: true });
    const timeout = window.setTimeout(() => finish(new Error("核心验证超时，请稍后重试")), 120_000);
    function finish(error?: Error) {
      window.clearTimeout(timeout);
      source.close();
      if (error) {reject(error);} else {resolve();}
    }
    source.addEventListener("snapshot", (event) => {
      const value: unknown = JSON.parse((event as MessageEvent<string>).data);
      const state = value && typeof value === "object" && "state" in value ? value.state : null;
      if (state === "SUCCEEDED") {finish();}
      if (state === "FAILED" || state === "CANCELLED") {finish(new Error("核心验证未通过"));}
    });
    source.addEventListener("succeeded", () => finish());
    source.addEventListener("failed", () => finish(new Error("核心验证未通过")));
    source.addEventListener("cancelled", () => finish(new Error("核心验证已取消")));
    source.onerror = () => finish(new Error("核心验证连接中断"));
  });
}

function property(value: unknown, name: string) {
  return value && typeof value === "object" && name in value ? Reflect.get(value, name) : undefined;
}

function readPlayUrl(value: unknown) {
  const playUrl = property(value, "playUrl");
  if (typeof playUrl !== "string") {throw new Error("启动响应缺少游戏地址");}
  const url = new URL(playUrl, window.location.origin);
  if (url.origin !== window.location.origin || !url.pathname.startsWith("/play/")) {throw new Error("启动响应包含无效地址");}
  url.searchParams.set("experience", "immersive");
  return `${url.pathname}${url.search}`;
}

export async function launchImmersiveGame(gameId: string, returnTo: string) {
  const body: LaunchRequest = {
    gameId,
    coreId: null,
    saveStateId: null,
    dosEntry: null,
    returnTo,
    clientCapabilities: {
      secureContext: window.isSecureContext,
      crossOriginIsolated: window.crossOriginIsolated,
      sharedArrayBuffer: typeof SharedArrayBuffer !== "undefined",
    },
  };
  for (let attempt = 0; attempt < 2; attempt += 1) {
    const response = await fetch("/api/v1/launches", {
      method: "POST",
      credentials: "same-origin",
      headers: await writeHeaders({ "Content-Type": "application/json", "Idempotency-Key": newUuid() }),
      body: JSON.stringify(body),
    });
    const result: unknown = await response.json();
    if (!response.ok) {throw new ImmersiveAPIError(response.status, errorMessage(result, "当前游戏无法启动"));}
    if (response.status === 202) {
      const jobId = property(result, "jobId");
      if (typeof jobId !== "string") {throw new Error("核心验证响应无效");}
      await waitForValidation(jobId);
      continue;
    }
    return readPlayUrl(result);
  }
  throw new Error("核心验证完成后仍无法启动");
}
