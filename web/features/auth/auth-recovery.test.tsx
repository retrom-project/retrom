import { act, cleanup, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { handleAuthenticationResponse } from "@/lib/api/client";
import { AuthProvider, useAuth } from "./auth-provider";
import { userStorageKey } from "./storage";
import type { AuthContext } from "./types";

const navigation = vi.hoisted(() => ({ replace: vi.fn(), refresh: vi.fn() }));
vi.mock("next/navigation", () => ({ useRouter: () => navigation }));
const account: AuthContext = {
  instanceState: "READY", mode: "test", authenticationState: "AUTHENTICATED",
  user: { userId: "user", username: "alice", displayName: "Alice", role: "USER" }, csrfToken: "csrf",
  idleExpiresAtMs: null, absoluteExpiresAtMs: null, testDefaultAccountActive: false,
};
const wrapper = ({ children }: { children: ReactNode }) => <AuthProvider initialContext={account}>{children}</AuthProvider>;
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.clearAllMocks(); localStorage.clear(); });

it("coalesces account rechecks and retains valid context and local preferences", async () => {
  let resolve: (response: Response) => void = () => undefined;
  const fetchMock = vi.fn(() => new Promise<Response>((done) => { resolve = done; }));
  vi.stubGlobal("fetch", fetchMock);
  const key = userStorageKey("user", "home", "pins")!; localStorage.setItem(key, "kept");
  const { result } = renderHook(useAuth, { wrapper });
  const expired = () => new Response(JSON.stringify({ error: { code: "AUTHENTICATION_REQUIRED" } }), { status: 401 });
  await act(async () => { await handleAuthenticationResponse(expired()); await handleAuthenticationResponse(expired()); });
  expect(fetchMock).toHaveBeenCalledOnce(); expect(result.current.recovery).toBe("pending");
  await act(async () => { resolve(new Response(JSON.stringify(account))); await result.current.recover(); });
  expect(result.current.context.user?.userId).toBe("user"); expect(localStorage.getItem(key)).toBe("kept");
  expect(navigation.replace).not.toHaveBeenCalled(); expect(result.current.recovery).toBe("idle");
});

it("exposes a recoverable network failure and clears storage only after confirmed revocation", async () => {
  const revoked = { ...account, authenticationState: "UNAUTHENTICATED", user: null, csrfToken: null };
  vi.stubGlobal("fetch", vi.fn().mockRejectedValueOnce(new TypeError("offline")).mockResolvedValueOnce(new Response(JSON.stringify(revoked))));
  const key = userStorageKey("user", "home", "pins")!; localStorage.setItem(key, "kept");
  const { result } = renderHook(useAuth, { wrapper });
  await act(async () => result.current.recover());
  expect(result.current.recovery).toBe("failed"); expect(result.current.context.user).toEqual(account.user);
  expect(localStorage.getItem(key)).toBe("kept"); expect(navigation.replace).not.toHaveBeenCalled();
  await act(async () => result.current.recover());
  expect(result.current.context.user).toBeNull(); expect(localStorage.getItem(key)).toBeNull();
  expect(navigation.replace).toHaveBeenCalledWith(expect.stringContaining("/login?returnTo="));
});
