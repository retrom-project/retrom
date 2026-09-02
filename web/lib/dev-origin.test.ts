import { describe, expect, it } from "vitest";
import { isCsrfOriginAllowed } from "next/dist/server/app-render/csrf-protection";
import { localDevOrigins } from "./dev-origin";

describe("localDevOrigins", () => {
  it.each(["localhost", "feature-a1b2c3d4e5f6.localhost", "launch.rpg.feature-a1b2c3d4e5f6.localhost"])(
    "allows localhost development resources from %s",
    (origin) => {
      expect(isCsrfOriginAllowed(origin, localDevOrigins())).toBe(true);
    },
  );

  it.each(["dev.example", "192.168.50.187", "null"])("rejects non-local origin %s", (origin) => {
    expect(isCsrfOriginAllowed(origin, localDevOrigins())).toBe(false);
  });

  it("uses a fixed localhost-only contract", () => {
    expect(localDevOrigins()).toEqual(["localhost", "*.localhost", "**.localhost"]);
  });
});
