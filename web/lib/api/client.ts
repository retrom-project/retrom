import createClient from "openapi-fetch";
import type { paths } from "./generated/schema";
const baseUrl =
  typeof window === "undefined" ? "http://127.0.0.1" : window.location.origin;
export const api = createClient<paths>({
  baseUrl,
  credentials: "same-origin",
  fetch: (request) => globalThis.fetch(request),
});
let csrfToken = "";
export function configureClient(token: string) {
  csrfToken = token;
}
export function writeHeaders(): Record<string, string> {
  return csrfToken ? { "X-Retrom-Csrf": csrfToken } : {};
}
api.use({
  onRequest({ request }) {
    if (!["GET", "HEAD", "OPTIONS"].includes(request.method)) {
      request.headers.set("X-Retrom-Csrf", csrfToken);
    }
  },
});
export class ApiError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly status: number,
  ) {
    super(
      code === "VERSION_CONFLICT" &&
        ["", "VERSION_CONFLICT", "version conflict", "Item changed; refresh and try again"].includes(message)
        ? "数据已变化或与现有记录冲突，请刷新后重试。"
        : message,
    );
  }
}
export function result<T>(response: {
  data?: T;
  error?: { code: string; message: string };
  response: Response;
}): T {
  if (response.error) {
    throw new ApiError(
      response.error.code,
      response.error.message,
      response.response.status,
    );
  }
  if (response.data === undefined) {
    throw new ApiError(
      "EMPTY_RESPONSE",
      "服务未返回结果。",
      response.response.status,
    );
  }
  return response.data;
}
export async function upload<T>(
  url: string,
  body: FormData,
  method: "POST" | "PUT" = "POST",
): Promise<T> {
  const response = await fetch(url, {
    method,
    credentials: "same-origin",
    headers: writeHeaders(),
    body,
  });
  const payload: unknown = await response.json();
  if (!response.ok) {
    const error = readError(payload);
    throw new ApiError(error.code, error.message, response.status);
  }
  return payload as T;
}
export function readError(payload: unknown) {
  if (
    payload &&
    typeof payload === "object" &&
    "code" in payload &&
    "message" in payload &&
    typeof payload.code === "string" &&
    typeof payload.message === "string"
  ) {
    return { code: payload.code, message: payload.message };
  }
  return { code: "REQUEST_FAILED", message: "请求失败，请重试。" };
}
