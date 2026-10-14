// @vitest-environment node
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { describe, expect, test, vi } from "vitest";

const origin = "https://retrom.test";
const source = readFileSync(new URL("./service-worker.js", import.meta.url), "utf8");
type CacheKey = Request | string;
type Listener = (event: Record<string, unknown>) => void;
function worker() {
  const listeners = new Map<string, Listener>();
  const data = new Map<string, Map<string, Response>>();
  const key = (value: CacheKey) => typeof value === "string" ? new URL(value, origin).href : value.url;
  const caches = {
    keys: async () => [...data.keys()],
    delete: vi.fn(async (name: string) => data.delete(name)),
    open: vi.fn(async (name: string) => {
      if (!data.has(name)) { data.set(name, new Map()); }
      const entries = data.get(name)!;
      return {
        match: async (input: CacheKey) => entries.get(key(input))?.clone(),
        put: async (input: CacheKey, response: Response) => { entries.set(key(input), response.clone()); },
        keys: async () => [...entries.keys()].map(url => new Request(url)),
        delete: async (input: CacheKey) => entries.delete(key(input)),
      };
    }),
  };
  const fetcher = vi.fn(async () => new Response("network", { headers: { "Cache-Control": "public, max-age=31536000, immutable" } }));
  const clients = {
    claim: vi.fn(async () => {}),
    matchAll: vi.fn(async () => [] as Array<{ url: string }>),
  };
  const skipWaiting = vi.fn(async () => {});
  runInNewContext(`const APP_VERSION = "current"; const CACHE_STATIC = true;\n${source}`, {
    self: { location: { origin }, clients, skipWaiting, addEventListener: (name: string, listener: Listener) => listeners.set(name, listener) },
    fetch: fetcher, caches, Request, Response, URL,
  });
  async function dispatch(type: string, input: Record<string, unknown> = {}) {
    const work: Promise<unknown>[] = [];
    let response: Promise<Response> | undefined;
    listeners.get(type)!({ ...input, waitUntil: (value: Promise<unknown>) => work.push(value), respondWith: (value: Promise<Response>) => { response = value; } });
    const result = await response;
    await Promise.all(work);
    return result;
  }
  const request = (path: string, options: Partial<Request> = {}) => ({
    url: new URL(path, origin).href, method: "GET", headers: new Headers(), mode: "cors", destination: "script", ...options,
  });
  return { data, caches, fetcher, clients, skipWaiting, dispatch, request };
}

describe("application Service Worker boundaries", () => {
  test("prepares a public fallback without activating an update", async () => {
    const app = worker();
    await app.dispatch("install");
    expect(app.skipWaiting).not.toHaveBeenCalled();
    expect(app.data.get("retrom-pwa-shell-current")?.has(`${origin}/offline`)).toBe(true);
    expect(app.fetcher.mock.calls.every(args => (args as unknown as [Request])[0].credentials === "omit")).toBe(true);
  });

  test("does not cache authenticated documents and preserves offline isolation headers", async () => {
    const app = worker();
    const request = app.request("/account", { mode: "navigate", destination: "document" });
    expect(await (await app.dispatch("fetch", { request }))?.text()).toBe("network");
    expect(app.data.size).toBe(0);
    const cache = await app.caches.open("retrom-pwa-shell-current");
    await cache.put("/offline", new Response("offline", { headers: { "Cross-Origin-Opener-Policy": "same-origin", "Cross-Origin-Embedder-Policy": "require-corp" } }));
    app.fetcher.mockRejectedValue(new TypeError("offline"));
    const response = await app.dispatch("fetch", { request });
    expect(await response?.text()).toBe("offline");
    expect(response?.headers.get("Cross-Origin-Embedder-Policy")).toBe("require-corp");
    expect(response?.headers.get("Cross-Origin-Opener-Policy")).toBe("same-origin");
  });

  test.each(["/api/v1/auth/context", "/api/v1/saves", "/runtime/providers/core/client.mjs", "/content/file", "/health/ready", "/__retrom/runtime-isolation/id/shell.html", "/run/id/index.html"])("never intercepts %s", async path => {
    const app = worker();
    expect(await app.dispatch("fetch", { request: app.request(path, { mode: "navigate", destination: "document" }) })).toBeUndefined();
    expect(app.fetcher).not.toHaveBeenCalled();
  });

  test("leaves writes, Range, RSC, cross-origin and embedded documents to their existing handlers", async () => {
    const app = worker();
    for (const request of [
      app.request("/account", { method: "POST" }),
      app.request("/_next/static/asset.js", { headers: new Headers({ Range: "bytes=0-4" }) }),
      app.request("/library?_rsc=xyz", { headers: new Headers({ RSC: "1" }) }),
      app.request("https://isolation.test/run/index.html", { mode: "navigate", destination: "document" }),
      app.request("/nested.html", { mode: "navigate", destination: "iframe" }),
    ]) {
      expect(await app.dispatch("fetch", { request })).toBeUndefined();
    }
    expect(app.fetcher).not.toHaveBeenCalled();
  });

  test("reuses immutable UI chunks, including the preceding revision", async () => {
    const app = worker();
    const request = app.request("/_next/static/chunks/hash.js");
    await app.dispatch("fetch", { request });
    await app.dispatch("fetch", { request });
    expect(app.fetcher).toHaveBeenCalledTimes(1);
    const old = await app.caches.open("retrom-pwa-static-previous");
    await old.put("/_next/static/chunks/old.js", new Response("previous"));
    expect(await (await app.dispatch("fetch", { request: app.request("/_next/static/chunks/old.js") }))?.text()).toBe("previous");
  });

  test("does not retain development responses and tolerates unavailable storage online", async () => {
    const app = worker();
    app.fetcher.mockResolvedValue(new Response("development", { headers: { "Cache-Control": "no-store" } }));
    await app.dispatch("fetch", { request: app.request("/_next/static/chunks/dev.js") });
    expect(app.data.size).toBe(0);
    app.caches.open.mockRejectedValue(new Error("quota"));
    expect(await (await app.dispatch("fetch", { request: app.request("/pwa/icons/icon-192.png") }))?.text()).toBe("development");
    app.fetcher.mockResolvedValue(new Response("production", { headers: { "Cache-Control": "immutable" } }));
    expect(await (await app.dispatch("fetch", { request: app.request("/_next/static/chunks/prod.js") }))?.text()).toBe("production");
  });

  test("blocks activation while another window is playing or immersive", async () => {
    const app = worker();
    const reply = vi.fn();
    for (const path of ["/play/run-id", "/immersive"]) {
      app.clients.matchAll.mockResolvedValue([{ url: origin + path }]);
      await app.dispatch("message", { data: { type: "RETROM_ACTIVATE_UPDATE" }, source: { url: origin + "/account" }, ports: [{ postMessage: reply }] });
      expect(reply).toHaveBeenLastCalledWith({ status: "playing" });
    }
    expect(app.skipWaiting).not.toHaveBeenCalled();
    expect(app.clients.matchAll).toHaveBeenCalledWith({ type: "window", includeUncontrolled: true });
    app.clients.matchAll.mockResolvedValue([{ url: origin + "/account" }]);
    await app.dispatch("message", { data: { type: "RETROM_ACTIVATE_UPDATE" }, source: { url: origin + "/account" }, ports: [{ postMessage: reply }] });
    expect(app.skipWaiting).toHaveBeenCalledOnce();
    expect(reply).toHaveBeenLastCalledWith({ status: "activating" });
  });

  test("cache retirement retains the previous UI version and all game caches", async () => {
    const app = worker();
    for (const name of ["retrom-content-io-v1", "retrom-unsynced-saves", "retrom-pwa-shell-old", "retrom-pwa-shell-previous", "retrom-pwa-shell-current"]) {
      await app.caches.open(name);
    }
    await app.dispatch("activate");
    expect(app.caches.delete).toHaveBeenCalledExactlyOnceWith("retrom-pwa-shell-old");
    expect(app.clients.claim).toHaveBeenCalledOnce();
  });
});
