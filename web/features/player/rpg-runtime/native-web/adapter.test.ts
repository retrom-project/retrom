import { describe, expect, it } from "vitest";
import { nativeBootstrapAction } from "./adapter";

describe("native RPG bootstrap reload", () => {
  it("connects when an authenticated bootstrap GET redirects directly to the bridge", () => {
    expect(nativeBootstrapAction("BOOTSTRAP", {
      type: "RETROM_RPG_NATIVE_BRIDGE_READY",
      protocolVersion: 1,
    })).toBe("CONNECT");
  });

  it("keeps the one-time ticket path for a first bootstrap", () => {
    const ready = { type: "RETROM_RPG_NATIVE_BOOTSTRAP_READY", protocolVersion: 1 };
    expect(nativeBootstrapAction("BOOTSTRAP", ready)).toBe("SEND_TICKET");
    expect(nativeBootstrapAction("BRIDGE", ready)).toBe("IGNORE");
    expect(nativeBootstrapAction("BRIDGE", {
      type: "RETROM_RPG_NATIVE_BRIDGE_READY",
      protocolVersion: 1,
    })).toBe("CONNECT");
  });

  it("rejects wrong versions, extra fields, arrays and unknown messages", () => {
    for (const value of [
      { type: "RETROM_RPG_NATIVE_BRIDGE_READY", protocolVersion: 2 },
      { type: "RETROM_RPG_NATIVE_BRIDGE_READY", protocolVersion: 1, launchId: "unexpected" },
      { type: "UNKNOWN", protocolVersion: 1 },
      ["RETROM_RPG_NATIVE_BRIDGE_READY", 1],
      null,
    ]) {
      expect(nativeBootstrapAction("BOOTSTRAP", value)).toBe("IGNORE");
    }
  });
});
