import type { components } from "@/lib/api/generated/schema";

export type AuthUser = components["schemas"]["AuthUser"];
export type AuthContext = components["schemas"]["AuthContext"];
export type AccountRole = AuthUser["role"];
export type AccountStatus = components["schemas"]["AdminUser"]["status"];

export type APIError = {
  error?: {
    code?: string;
    message?: string;
    details?: { reasonCode?: string };
    requestId?: string;
  };
};

export async function readAPIError(response: Response, fallback: string) {
  const payload = await response.json().catch(() => null) as APIError | null;
  return {
    code: payload?.error?.code ?? "REQUEST_FAILED",
    message: payload?.error?.message ?? fallback,
    reasonCode: payload?.error?.details?.reasonCode,
    requestId: payload?.error?.requestId
  };
}
