import { afterEach, describe, expect, it, vi } from "vitest";
import { safeReturnTo } from "./auth-provider";
import { clearUserStorage, userStorageKey } from "./storage";
import { configureAuthenticatedClient, handleAuthenticationResponse, writeHeaders } from "@/lib/api/client";

afterEach(() => {
  localStorage.clear(); sessionStorage.clear();
  configureAuthenticatedClient({ csrfToken: null, onAuthenticationFailure: null });
});

describe("authentication browser boundaries", () => {
  it("accepts only internal, non-authentication return targets", () => {
    expect(safeReturnTo("/library?q=gba")).toBe("/library?q=gba");
    expect(safeReturnTo("//evil.example/path")).toBe("/");
    expect(safeReturnTo("https://evil.example/path")).toBe("/");
    expect(safeReturnTo("/login?returnTo=/admin")).toBe("/");
  });

  it("adds the in-memory CSRF token and reacts to authentication expiry", () => {
    const expired = vi.fn();
    configureAuthenticatedClient({ csrfToken: "csrf-memory-only", onAuthenticationFailure: expired });
    expect(writeHeaders({ "If-Match": '"v1"' })).toEqual({ "If-Match": '"v1"', "X-Retrom-Csrf": "csrf-memory-only" });
    handleAuthenticationResponse(new Response(null, { status: 401 }));
    expect(expired).toHaveBeenCalledOnce();
  });

  it("clears only the current user's namespaced browser state", () => {
    const first = userStorageKey("user-1", "home", "pinned-platforms")!;
    const second = userStorageKey("user-2", "home", "pinned-platforms")!;
    localStorage.setItem(first, "one"); localStorage.setItem(second, "two");
    sessionStorage.setItem(userStorageKey("user-1", "reviews", "queue")!, "queue");
    clearUserStorage("user-1");
    expect(localStorage.getItem(first)).toBeNull();
    expect(sessionStorage.length).toBe(0);
    expect(localStorage.getItem(second)).toBe("two");
  });
});
