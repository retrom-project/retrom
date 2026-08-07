import { describe, expect, it } from "vitest";
import { statusTone } from "./status";

describe("statusTone", () => {
  it("keeps success, progress, warning, and blocking states semantically distinct", () => {
    expect(statusTone("READY")).toBe("good");
    expect(statusTone("NEEDS_VALIDATION")).toBe("info");
    expect(statusTone("HASH_WARNING")).toBe("warn");
    expect(statusTone("DEPENDENCY_MISSING")).toBe("bad");
    expect(statusTone("INCOMPATIBLE")).toBe("bad");
    expect(statusTone("unrecognized")).toBe("neutral");
  });
});
