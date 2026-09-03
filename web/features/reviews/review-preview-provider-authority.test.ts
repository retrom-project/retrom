import {readFileSync} from "node:fs";
import {describe, expect, it} from "vitest";

describe("review preview Provider authority", () => {
  it("mounts every preview through the shared Provider controller", () => {
    const source = readFileSync("features/reviews/review-preview-player.tsx", "utf8");
    expect(source).toContain('from "@/features/player/runtime/runtime-controller"');
    expect(source.match(/mountProviderRuntime\(/gu)).toHaveLength(1);
    expect(source).not.toMatch(/@xxxsen\/retrom-runtime|runtimeFamily|mountEmulatorJS|adapters\/ejs/u);
  });
});
