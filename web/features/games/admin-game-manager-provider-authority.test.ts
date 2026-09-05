import {readFileSync} from "node:fs";
import {describe, expect, it} from "vitest";

describe("admin game manager Provider authority", () => {
  it("does not project retired runtime artifact selection into the UI model", () => {
    const source = readFileSync("features/games/admin-game-manager.tsx", "utf8");
    expect(source).not.toMatch(/coreArtifactId|routeKey|runtimeFamily|adapterAbi/u);
  });
});
