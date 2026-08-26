import { describe, expect, it } from "vitest";
import { playerFrameSource } from "./rpg-runtime-csp";

const launchId = "0198abcd-1234-7123-8abc-1234567890ab";

describe("playerFrameSource", () => {
  it("allows only the current player Launch exact runtime origin", () => {
    expect(playerFrameSource(
      `/play/${launchId}`,
      "https://{launchId}.rpg-runtime.dev.sendev.cc",
    )).toBe(`'self' https://${launchId}.rpg-runtime.dev.sendev.cc`);
    expect(playerFrameSource("/admin/reviews/item", "https://{launchId}.rpg-runtime.dev.sendev.cc"))
      .toBe("'self'");
  });

  it("fails closed for malformed or non-canonical templates", () => {
    expect(playerFrameSource(`/play/${launchId}`, "https://runtime.invalid/game/{launchId}"))
      .toBe("'self'");
    expect(playerFrameSource(`/play/${launchId}`, "javascript:{launchId}"))
      .toBe("'self'");
    expect(playerFrameSource(`/play/${launchId}`, "https://runtime.invalid"))
      .toBe("'self'");
  });
});
