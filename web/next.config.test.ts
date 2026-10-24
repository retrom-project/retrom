import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { afterEach, describe, expect, test, vi } from "vitest";

import nextConfig, { backendProxyLimits } from "./next.config";

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
});

test("keeps the development indicator out of acceptance interactions", async () => {
  vi.stubEnv("NEXT_WEB_E2E", "true");
  vi.resetModules();
  expect((await import("./next.config")).default.devIndicators).toBe(false);
});

test("retains the development indicator during ordinary development", async () => {
  vi.stubEnv("NEXT_WEB_E2E", "");
  vi.resetModules();
  expect((await import("./next.config")).default.devIndicators).toEqual({
    position: "bottom-right",
  });
});

describe("backend rewrite proxy limits", () => {
  test("keeps replacement submission on the ordinary bounded proxy instead of waiting for content preparation", async () => {
    expect(nextConfig.experimental?.proxyTimeout).toBe(300_000);
    expect(await nextConfig.rewrites?.()).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ source: "/api/:path*" }),
      ]),
    );
  });

  test("does not statically bundle the runtime implementation package", () => {
    expect(nextConfig.transpilePackages ?? []).not.toContain(
      "@xxxsen/retrom-runtime",
    );
    const manifest = JSON.parse(
      readFileSync(resolve(process.cwd(), "package.json"), "utf8"),
    ) as {
      dependencies?: Record<string, string>;
    };
    expect(manifest.dependencies?.["@xxxsen/retrom-runtime"]).toBeUndefined();
  });

  test("forwards the largest supported save-state body without the Next.js defaults truncating it", () => {
    expect(backendProxyLimits.bodyBytes).toBe(283_115_520);
    expect(nextConfig.experimental?.proxyClientMaxBodySize).toBe(
      backendProxyLimits.bodyBytes,
    );
    expect(nextConfig.experimental?.proxyTimeout).toBe(
      backendProxyLimits.timeoutMs,
    );
  });

  test("applies the limits to the runtime backend rewrite", async () => {
    const rewrites = await nextConfig.rewrites?.();

    expect(rewrites).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ source: "/runtime/:path*" }),
      ]),
    );
  });

  test.each([
    "docker/nginx.conf.example",
    "scripts/pfb/gateway/nginx.conf",
  ])("keeps %s from closing content replacement before the Web proxy", (path) => {
    const configuration = readFileSync(resolve(process.cwd(), "..", path), "utf8");
    const timeout = /proxy_read_timeout\s+(\d+)s;/u.exec(configuration);

    expect(timeout).not.toBeNull();
    expect(Number(timeout?.[1]) * 1000).toBeGreaterThanOrEqual(
      backendProxyLimits.timeoutMs,
    );
  });
});
