import { describe, expect, it } from "vitest";
import { isCsrfOriginAllowed } from "next/dist/server/app-render/csrf-protection";
import { unrestrictedDevOrigins } from "./dev-origin";

describe("unrestrictedDevOrigins", () => {
  it.each(["dev.sendev.cc", "an-unconfigured.example", "192.168.50.187", "null"])(
    "allows development resources from %s",
    (origin) => {
      expect(isCsrfOriginAllowed(origin, unrestrictedDevOrigins())).toBe(true);
    },
  );

  it("does not depend on RETROM_PUBLIC_ORIGIN", () => {
    expect(unrestrictedDevOrigins()).toEqual(["**.*", "null"]);
  });
});
