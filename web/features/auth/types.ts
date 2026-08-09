export type AccountRole = "ADMIN" | "USER";
export type AccountStatus = "ENABLED" | "DISABLED" | "DELETED";

export type AuthUser = {
  userId: string;
  username: string;
  displayName: string;
  role: AccountRole;
};

export type AuthContext = {
  instanceState: "INITIALIZATION_REQUIRED" | "READY";
  mode: "release" | "test";
  authenticationState: "NOT_APPLICABLE" | "UNAUTHENTICATED" | "AUTHENTICATED";
  user: AuthUser | null;
  csrfToken: string | null;
  idleExpiresAtMs: number | null;
  absoluteExpiresAtMs: number | null;
  testDefaultAccountActive: boolean;
};

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
