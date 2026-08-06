import { describe, expect, it } from "vitest";
import { allowedDevOriginsFromPublicOrigin } from "./dev-origin";

describe("allowedDevOriginsFromPublicOrigin", () => {
  it("maps the configured development domain to the Next.js allowlist", () => {
    expect(allowedDevOriginsFromPublicOrigin("http://local.sendev.cc:3000")).toEqual(["local.sendev.cc"]);
  });

  it("does not create an allowlist without a configured public origin", () => {
    expect(allowedDevOriginsFromPublicOrigin(undefined)).toBeUndefined();
  });

  it.each([
    "local.sendev.cc:3000",
    "ftp://local.sendev.cc:3000",
    "http://user@local.sendev.cc:3000",
    "http://local.sendev.cc:3000/path",
  ])("rejects a value that is not a single HTTP(S) origin: %s", (value) => {
    expect(() => allowedDevOriginsFromPublicOrigin(value)).toThrow("RETROM_PUBLIC_ORIGIN");
  });
});
