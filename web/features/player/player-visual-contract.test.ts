import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

function cssRule(source: string, selector: string) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = source.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
  expect(match, `missing CSS rule ${selector}`).not.toBeNull();
  return match?.[1] ?? "";
}

describe("Player centered reveal target", () => {
  it("keeps the pointer target at the visible handle, leaving the game edges clickable", () => {
    const source = readFileSync(resolve(process.cwd(), "features/player/player.css"), "utf8");
    const rule = cssRule(source, ".player-hud-handle");
    expect(rule).toContain("left: 50%");
    expect(rule).toContain("width: 48px");
    expect(rule).toContain("height: 44px");
    expect(source).not.toMatch(/\.player-hud-handle\[aria-pressed="false"\]\s*\{[^}]*width: auto/);
  });
});
