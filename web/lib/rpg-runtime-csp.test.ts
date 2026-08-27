import { describe, expect, it } from "vitest";
import { playerFrameSource } from "./rpg-runtime-csp";

describe("playerFrameSource", () => {
  it("allows only the configured runtime hostname family on every app document", () => {
    expect(playerFrameSource("https://{launchId}.rpg-runtime.dev.sendev.cc"))
      .toBe("'self' https://*.rpg-runtime.dev.sendev.cc");
    expect(playerFrameSource("http://{launchId}.rpg.localhost:18092"))
      .toBe("'self' http://*.rpg.localhost:18092");
  });

  it("fails closed for malformed or non-canonical templates", () => {
    expect(playerFrameSource("https://runtime.invalid/game/{launchId}")).toBe("'self'");
    expect(playerFrameSource("javascript:{launchId}")).toBe("'self'");
    expect(playerFrameSource("https://runtime.invalid")).toBe("'self'");
    expect(playerFrameSource("https://runtime-{launchId}.invalid")).toBe("'self'");
    expect(playerFrameSource("https://user:secret@{launchId}.runtime.invalid")).toBe("'self'");
  });
});
