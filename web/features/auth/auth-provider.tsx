"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { usePathname, useRouter } from "next/navigation";
import { clearUserStorage } from "./storage";
import type { AuthContext } from "./types";
import { configureAuthenticatedClient, handleAuthenticationResponse, writeHeaders } from "@/lib/api/client";

type AuthState = {
  context: AuthContext;
  refresh: () => Promise<AuthContext>;
  acceptContext: (context: AuthContext) => void;
  authenticatedFetch: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;
  logout: () => Promise<void>;
};

const Context = createContext<AuthState | null>(null);

const publicRoutes = new Set(["/setup", "/login", "/register", "/reset-password"]);

export function safeReturnTo(value: string | null) {
  if (!value || !value.startsWith("/") || value.startsWith("//")) return "/";
  try {
    const parsed = new URL(value, "http://retrom.local");
    if (parsed.origin !== "http://retrom.local" || publicRoutes.has(parsed.pathname)) return "/";
    return `${parsed.pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return "/";
  }
}

function currentReturnTo() {
  if (typeof window === "undefined") return "/";
  return safeReturnTo(`${window.location.pathname}${window.location.search}`);
}

export function AuthProvider({ initialContext, children }: { initialContext: AuthContext; children: ReactNode }) {
  const [context, setContext] = useState(initialContext);
  const contextRef = useRef(context);
  const pathname = usePathname();
  const router = useRouter();

  const acceptContext = useCallback((next: AuthContext) => {
    contextRef.current = next;
    setContext(next);
  }, []);

  const clearAndLogin = useCallback(() => {
    clearUserStorage(contextRef.current.user?.userId);
    const next = { ...contextRef.current, authenticationState: "UNAUTHENTICATED" as const, user: null, csrfToken: null };
    acceptContext(next);
    router.replace(`/login?returnTo=${encodeURIComponent(currentReturnTo())}`);
  }, [acceptContext, router]);

  useEffect(() => {
    configureAuthenticatedClient({ csrfToken: context.csrfToken, onAuthenticationFailure: clearAndLogin });
    return () => configureAuthenticatedClient({ csrfToken: null, onAuthenticationFailure: null });
  }, [clearAndLogin, context.csrfToken]);

  const refresh = useCallback(async () => {
    const response = await fetch("/api/v1/auth/context", { cache: "no-store", credentials: "same-origin" });
    if (!response.ok) throw new Error(`认证上下文请求失败（HTTP ${response.status}）`);
    const next = await response.json() as AuthContext;
    acceptContext(next);
    return next;
  }, [acceptContext]);

  const authenticatedFetch = useCallback(async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const method = (init.method ?? "GET").toUpperCase();
    const headers = new Headers(init.headers);
    if (!["GET", "HEAD", "OPTIONS"].includes(method)) {
      for (const [name, value] of Object.entries(writeHeaders())) headers.set(name, value);
    }
    const response = await fetch(input, { ...init, headers, credentials: "same-origin" });
    return handleAuthenticationResponse(response);
  }, []);

  const logout = useCallback(async () => {
    const response = await authenticatedFetch("/api/v1/auth/logout", { method: "POST" });
    if (!response.ok && response.status !== 401) throw new Error("退出登录失败，请刷新后重试");
    clearUserStorage(contextRef.current.user?.userId);
    acceptContext({ ...contextRef.current, authenticationState: "UNAUTHENTICATED", user: null, csrfToken: null });
    router.replace("/login");
    router.refresh();
  }, [acceptContext, authenticatedFetch, router]);

  useEffect(() => {
    if (pathname.startsWith("/play/")) return;
    const publicRoute = publicRoutes.has(pathname);
    if (context.instanceState === "INITIALIZATION_REQUIRED") {
      if (pathname !== "/setup") router.replace("/setup");
      return;
    }
    if (context.authenticationState !== "AUTHENTICATED") {
      if (!publicRoute) router.replace(`/login?returnTo=${encodeURIComponent(currentReturnTo())}`);
      return;
    }
    if (publicRoute) router.replace("/");
  }, [context.authenticationState, context.instanceState, pathname, router]);

  const value = useMemo(() => ({ context, refresh, acceptContext, authenticatedFetch, logout }), [acceptContext, authenticatedFetch, context, logout, refresh]);
  return <Context.Provider value={value}>{children}</Context.Provider>;
}

export function useAuth() {
  const value = useContext(Context);
  if (!value) throw new Error("useAuth 必须在 AuthProvider 中使用");
  return value;
}
