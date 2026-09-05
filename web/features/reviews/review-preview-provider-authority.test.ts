import {readFileSync} from "node:fs";
import {describe, expect, it} from "vitest";

describe("review preview Provider authority", () => {
  it("uses the ordinary Player shell instead of owning another runtime lifecycle", () => {
    const source = readFileSync("features/reviews/review-preview-player.tsx", "utf8");
    expect(source).toContain('from "@/features/player/player-shell"');
    expect(source).toContain('<PlayerShell launchId={previewId}');
    expect(source).not.toMatch(/mountProviderRuntime|setTimeout|useEffect|RuntimeController/u);
    expect(source).not.toMatch(/@xxxsen\/retrom-runtime|runtimeFamily|mountEmulatorJS|adapters\/ejs/u);
  });
});
