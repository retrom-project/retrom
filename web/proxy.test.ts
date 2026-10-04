// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { config, proxy } from "./proxy";
import { unstable_doesMiddlewareMatch } from "next/experimental/testing/server";

const anonymous = {
  instanceState: "READY", mode: "release", authenticationState: "UNAUTHENTICATED",
  csrfToken: null, user: null, idleExpiresAtMs: null, absoluteExpiresAtMs: null, testDefaultAccountActive: false
};

function backendResponses(configuration: () => Response | Promise<Response>, authenticated = false) {
  const context = authenticated ? {
    ...anonymous, authenticationState: "AUTHENTICATED",
    user: { userId: "user", username: "user", displayName: "User", role: "USER" }
  } : anonymous;
  const fetcher = vi.fn(async (input: string) => input.endsWith("/web-config")
    ? configuration() : Response.json(context));
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

afterEach(() => { vi.unstubAllGlobals(); vi.unstubAllEnvs(); });

describe("document policy from backend configuration", () => {
  it("uses backend configuration even when frontend environment disagrees", async () => {
    vi.stubEnv("RETROM_RPG_RUNTIME_ORIGIN_TEMPLATE", "https://{launchId}.wrong.example.com");
    const fetcher = backendResponses(() => Response.json({ runtimeOriginTemplate: "https://{launchId}.example.com" }));
    const response = await proxy(new NextRequest("https://example.com/login", {
      headers: { Cookie: "session=fixture", "X-Forwarded-Host": "untrusted.example" }
    }));
    expect(response.status).toBe(200);
    const policy = response.headers.get("Content-Security-Policy");
    expect(policy).toContain("frame-src 'self' https://*.example.com;");
    expect(policy).not.toContain("wrong.example.com");
    expect(policy).not.toContain("untrusted.example");
    expect(response.headers.get("x-middleware-request-content-security-policy")).toBe(policy);
    const nonce = response.headers.get("x-middleware-request-x-nonce");
    expect(nonce).toBeTruthy();
    expect(policy).toContain(`'nonce-${nonce}'`);
    expect(response.headers.get("Cache-Control")).toBe("private, no-store");
    expect(fetcher).toHaveBeenCalledWith("http://127.0.0.1:8080/api/v1/web-config", expect.objectContaining({
      cache: "no-store", redirect: "error", headers: { Accept: "application/json" }, signal: expect.any(AbortSignal)
    }));
    expect(fetcher).toHaveBeenCalledWith("http://127.0.0.1:8080/api/v1/auth/context", expect.objectContaining({
      headers: { Accept: "application/json", Cookie: "session=fixture" }
    }));
  });

  it("refreshes configuration and nonce on the next document request", async () => {
    let template = "https://{launchId}.example.com";
    backendResponses(() => Response.json({ runtimeOriginTemplate: template }));
    const first = await proxy(new NextRequest("https://example.com/login"));
    template = "https://{launchId}.games.example.com:8443";
    const second = await proxy(new NextRequest("https://example.com/login"));
    expect(first.headers.get("Content-Security-Policy")).toContain("https://*.example.com;");
    expect(second.headers.get("Content-Security-Policy")).toContain("https://*.games.example.com:8443;");
    expect(second.headers.get("x-middleware-request-x-nonce")).not.toBe(first.headers.get("x-middleware-request-x-nonce"));
  });

  it.each([
    [false, "/", 307, "location"],
    [true, "/admin/users", 200, "x-middleware-rewrite"],
    [true, "/", 200, "x-middleware-next"]
  ] as const)("preserves auth routing for authenticated=%s, path=%s", async (authenticated, path, status, header) => {
    backendResponses(() => Response.json({ runtimeOriginTemplate: "https://{launchId}.example.com" }), authenticated);
    const response = await proxy(new NextRequest(`https://example.com${path}`));
    expect(response.status).toBe(status);
    expect(response.headers.get(header)).toBeTruthy();
    expect(response.headers.get("Content-Security-Policy")).toContain("frame-src 'self' https://*.example.com;");
  });

  it.each([null, {}, { runtimeOriginTemplate: "" }, { runtimeOriginTemplate: 123 },
    { runtimeOriginTemplate: "https://{launchId}.example.com", unexpected: true },
    { runtimeOriginTemplate: "https://runtime.example.com/{launchId}" }
  ])("returns a closed 503 for invalid configuration %j", async (value) => {
    backendResponses(() => Response.json(value));
    const response = await proxy(new NextRequest("https://example.com/login"));
    expect(response.status).toBe(503);
    expect(response.headers.get("Content-Security-Policy")).toContain("frame-src 'self';");
    expect(response.headers.get("x-middleware-next")).toBeNull();
    expect(response.headers.get("Cache-Control")).toBe("private, no-store");
  });

  it.each(["unavailable", "redirect", "network", "timeout", "malformed"])("fails closed when configuration is %s", async (failure) => {
    backendResponses(() => {
      if (failure === "network") {throw new TypeError("network failure");}
      if (failure === "timeout") {throw new DOMException("timeout", "TimeoutError");}
      if (failure === "redirect") {return new Response(null, { status: 302, headers: { Location: "https://untrusted.example" } });}
      return failure === "malformed" ? new Response("{") : new Response("unavailable", { status: 503 });
    });
    expect((await proxy(new NextRequest("https://example.com/login"))).status).toBe(503);
  });
});

const matchesProxy = (url: string) => unstable_doesMiddlewareMatch({ config, nextConfig: {}, url });

describe("authentication proxy matcher", () => {
  it.each(["/icon.svg", "/icon.svg?revision=abc"])("serves the favicon without authentication: %s", url => {
    expect(matchesProxy(url)).toBe(false);
  });

  it.each(["/", "/library", "/admin/users", "/icon.svg/private", "/iconXsvg", "/icon.svg-extra"])("keeps page and lookalike paths behind authentication: %s", url => {
    expect(matchesProxy(url)).toBe(true);
  });
});
