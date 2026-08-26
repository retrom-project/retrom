import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, test } from "vitest";

import nextConfig, { backendProxyLimits } from "./next.config";

describe("backend rewrite proxy limits", () => {
  test("forwards the largest supported save-state body without the Next.js defaults truncating or timing it out", () => {
    expect(backendProxyLimits).toEqual({
      bodyBytes: 283_115_520,
      timeoutMs: 300_000
    });
    expect(nextConfig.experimental?.proxyClientMaxBodySize).toBe(backendProxyLimits.bodyBytes);
    expect(nextConfig.experimental?.proxyTimeout).toBe(backendProxyLimits.timeoutMs);
  });

  test("matches the formal save-state transport contract", () => {
    const contract = readFileSync(resolve(process.cwd(), "../docs/http-api-contract.md"), "utf8");

    expect(contract).toContain("`283115520` bytes（270 MiB）");
    expect(contract).toContain("`dev.sendev.cc` 的 NG 根 location 与 Next.js 全局 rewrite 代理层");
    expect(contract).toContain("300 秒 read/send/backend timeout");
    expect(contract).toContain("不对 `/api/v1/admin/imports` 或 save-state 增加独立 NG location");
  });

  test("applies the limits to the runtime backend rewrite", async () => {
    const rewrites = await nextConfig.rewrites?.();

    expect(rewrites).toEqual(expect.arrayContaining([
      expect.objectContaining({ source: "/runtime/:path*" })
    ]));
  });
});
