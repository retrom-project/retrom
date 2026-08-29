import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

function cssRule(source: string, selector: string) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
  expect(match, `missing CSS rule ${selector}`).not.toBeNull();
  return match?.[1] ?? "";
}

describe("Player top-edge reveal target", () => {
  it("covers the complete 32px viewport edge instead of only the center handle", () => {
    const source = readFileSync(resolve(process.cwd(), "features/player/player.css"), "utf8");
    const rule = cssRule(source, ".player-hud-handle[aria-pressed=\"false\"]");

    expect(rule).toContain("left: 0");
    expect(rule).toContain("right: 0");
    expect(rule).toContain("height: 32px");
    expect(rule).toContain("width: auto");
  });
});
