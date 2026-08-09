import { describe, expect, it } from "vitest";
import { decideAuthRoute, safeReturnTo } from "./routing";
import type { AuthContext } from "./types";

const anonymous: AuthContext = {
  instanceState: "READY",
  authenticationState: "UNAUTHENTICATED",
  mode: "release",
  user: null,
  csrfToken: null,
  idleExpiresAtMs: null,
  absoluteExpiresAtMs: null,
  testDefaultAccountActive: false
};

const administrator: AuthContext = {
  ...anonymous,
  authenticationState: "AUTHENTICATED",
  user: { userId: "admin", username: "admin", displayName: "Admin", role: "ADMIN" },
  csrfToken: "csrf"
};

describe("safeReturnTo", () => {
  it("accepts only local, non-authentication destinations", () => {
    expect(safeReturnTo("/library?q=gba")).toBe("/library?q=gba");
    expect(safeReturnTo("//evil.example/path")).toBe("/");
    expect(safeReturnTo("https://evil.example/path")).toBe("/");
    expect(safeReturnTo("/login?returnTo=/admin")).toBe("/");
  });
});

describe("decideAuthRoute", () => {
  it("routes an uninitialized instance only to setup", () => {
    const pending = { ...anonymous, instanceState: "INITIALIZATION_REQUIRED" as const };
    expect(decideAuthRoute(pending, "/", "/")).toEqual({ kind: "redirect", destination: "/setup" });
    expect(decideAuthRoute(pending, "/setup", "/setup")).toEqual({ kind: "allow" });
  });

  it("preserves a safe return target for anonymous protected requests", () => {
    expect(decideAuthRoute(anonymous, "/library", "/library?q=gba")).toEqual({
      kind: "redirect",
      destination: "/login?returnTo=%2Flibrary%3Fq%3Dgba"
    });
    expect(decideAuthRoute(anonymous, "/login", "/login")).toEqual({ kind: "allow" });
  });

  it("keeps authenticated users out of authentication pages", () => {
    expect(decideAuthRoute(administrator, "/login", "/login")).toEqual({ kind: "redirect", destination: "/" });
    expect(decideAuthRoute(administrator, "/admin/users", "/admin/users")).toEqual({ kind: "allow" });
  });

  it("renders the forbidden experience before a regular user enters admin routes", () => {
    const user = { ...administrator, user: { ...administrator.user!, role: "USER" as const } };
    expect(decideAuthRoute(user, "/admin/users", "/admin/users")).toEqual({ kind: "forbidden" });
  });
});
