import { describe, expect, test } from "vitest";

import nextConfig, { backendProxyLimits } from "./next.config";

describe("backend rewrite proxy limits", () => {
  test("forwards the largest supported save-state body without the Next.js defaults truncating or timing it out", () => {
    expect(backendProxyLimits).toEqual({
      bodyBytes: 75 * 1024 * 1024,
      timeoutMs: 150_000
    });
    expect(nextConfig.experimental?.proxyClientMaxBodySize).toBe(backendProxyLimits.bodyBytes);
    expect(nextConfig.experimental?.proxyTimeout).toBe(backendProxyLimits.timeoutMs);
  });

  test("applies the limits to the runtime backend rewrite", async () => {
    const rewrites = await nextConfig.rewrites?.();

    expect(rewrites).toEqual(expect.arrayContaining([
      expect.objectContaining({ source: "/runtime/:path*" })
    ]));
  });
});
