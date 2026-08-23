import type { components } from "@/lib/api/generated/schema";
import { newUuid } from "@/lib/crypto";
import { readAPIError } from "@/features/auth/types";

export type NetplayRoom = components["schemas"]["NetplayRoom"];
export type NetplayGame = components["schemas"]["NetplayGameSummary"];
export type NetplayRoomList = components["schemas"]["NetplayRoomList"];
export type NetplayGameList = components["schemas"]["NetplayGameList"];
export type NetplayLaunch = components["schemas"]["NetplayLaunchResponseBody"];
export type AuthenticatedFetch = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

export class NetplayAPIError extends Error {
  constructor(public readonly code: string, message: string) { super(message); }
}

export async function roomMutation<T = NetplayRoom>(
  authenticatedFetch: AuthenticatedFetch,
  path: string,
  method: "POST" | "PUT" | "DELETE",
  options: { version?: number; body?: unknown } = {},
) {
  const headers = new Headers({ "Idempotency-Key": newUuid() });
  if (options.version !== undefined) {headers.set("If-Match", `"v${options.version}"`);}
  if (options.body !== undefined) {headers.set("Content-Type", "application/json");}
  const response = await authenticatedFetch(path, {
    method,
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  });
  if (!response.ok) {
    const error = await readAPIError(response, `联机请求失败（HTTP ${response.status}）`);
    throw new NetplayAPIError(error.code, error.message);
  }
  if (response.status === 204) {return null as T;}
  return response.json() as Promise<T>;
}

export function applyRoomSnapshot(current: NetplayRoom, incoming: NetplayRoom) {
  if (incoming.roomId !== current.roomId) {return { room: current, gap: true };}
  if (incoming.version <= current.version) {return { room: current, gap: false };}
  return { room: incoming, gap: incoming.version > current.version + 1 };
}

export function netplayBlocker(code: NetplayGame["blockerCode"]) {
  return ({
    CONTENT_NOT_ALLOWLISTED: "当前内容类型尚未支持联机",
    CORE_NOT_ALLOWLISTED: "当前平台与核心组合尚未验证联机",
    DEPENDENCY_STALE: "运行依赖需要重新验证",
    GAME_UNAVAILABLE: "游戏当前不可用",
  } as const)[code ?? "CONTENT_NOT_ALLOWLISTED"];
}
