import type { AuthContext } from "./types";

export const publicAuthRoutes = new Set(["/setup", "/login", "/register", "/reset-password"]);

export type AuthRouteDecision =
  | { kind: "allow" }
  | { kind: "redirect"; destination: string }
  | { kind: "forbidden" };

export function safeReturnTo(value: string | null) {
  if (!value || !value.startsWith("/") || value.startsWith("//")) return "/";
  try {
    const parsed = new URL(value, "http://retrom.local");
    if (parsed.origin !== "http://retrom.local" || publicAuthRoutes.has(parsed.pathname)) return "/";
    return `${parsed.pathname}${parsed.search}${parsed.hash}`;
  } catch {
    return "/";
  }
}

export function decideAuthRoute(context: AuthContext, pathname: string, returnTo: string): AuthRouteDecision {
  if (pathname.startsWith("/play/")) return { kind: "allow" };
  if (context.instanceState === "INITIALIZATION_REQUIRED") {
    return pathname === "/setup" ? { kind: "allow" } : { kind: "redirect", destination: "/setup" };
  }
  const publicRoute = publicAuthRoutes.has(pathname);
  if (context.authenticationState !== "AUTHENTICATED") {
    return publicRoute
      ? { kind: "allow" }
      : { kind: "redirect", destination: `/login?returnTo=${encodeURIComponent(safeReturnTo(returnTo))}` };
  }
  if (publicRoute) return { kind: "redirect", destination: "/" };
  if (pathname.startsWith("/admin") && context.user?.role !== "ADMIN") return { kind: "forbidden" };
  return { kind: "allow" };
}
