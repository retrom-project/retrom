import {readFileSync} from "node:fs";
import {describe, expect, it} from "vitest";

describe("Player Provider bootstrap authority", () => {
  it("has one production dispatcher and no family or adapter bootstrap imports", () => {
    const source = readFileSync("features/player/player-bootstrap.ts", "utf8");
    expect(source).toContain('from "./runtime/runtime-controller"');
    expect(source).not.toMatch(/from "\.\/(?:adapters\/|rpg-runtime\/|player-bootstrap-retrom-runtime)/u);
    expect(source).not.toMatch(/is(?:Ons|KiriKiri|Butterscotch|TyranoScript|WASM4|RetromRpg)LaunchConfig/u);
    expect(source.match(/mountProviderRuntime\(/gu)).toHaveLength(1);
  });
});
