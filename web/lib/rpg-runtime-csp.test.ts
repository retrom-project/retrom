import { describe, expect, it } from "vitest";
import { playerFrameSource } from "./rpg-runtime-csp";

describe("playerFrameSource", () => {
  it("allows only the configured runtime hostname family on every app document", () => {
    expect(playerFrameSource("https://{launchId}.runtime.retrom.example"))
      .toBe("'self' https://*.runtime.retrom.example");
    expect(playerFrameSource("http://{launchId}.rpg.feature-a1b2c3d4e5f6.localhost:3000"))
      .toBe("'self' http://*.rpg.feature-a1b2c3d4e5f6.localhost:3000");
    expect(playerFrameSource("http://{launchId}.rpg.localhost:18092"))
      .toBe("'self' http://*.rpg.localhost:18092");
  });

  it("accepts the default host family and preserves non-default ports", () => {
    expect(playerFrameSource("https://{launchId}.example.com")).toBe("'self' https://*.example.com");
    expect(playerFrameSource("https://{launchId}.play.example.com:8443")).toBe("'self' https://*.play.example.com:8443");
    expect(playerFrameSource("https://{launchId}.example.com:443")).toBe("'self' https://*.example.com");
  });

  it.each([undefined, "", "https://{launchId}.example.com/", "https://{launchId}.example.com?",
    "https://{launchId}.example.com#", "https://{launchId}.example.com;script-src", "https://{launchId}.example..com"])(
    "rejects missing or malformed template %s", (template) => expect(playerFrameSource(template)).toBeNull()
  );

  it("fails closed for malformed or non-canonical templates", () => {
    expect(playerFrameSource("https://runtime.invalid/game/{launchId}")).toBeNull();
    expect(playerFrameSource("javascript:{launchId}")).toBeNull();
    expect(playerFrameSource("https://runtime.invalid")).toBeNull();
    expect(playerFrameSource("https://runtime-{launchId}.invalid")).toBeNull();
    expect(playerFrameSource("https://user:secret@{launchId}.runtime.invalid")).toBeNull();
  });
});
