import { describe, expect, it } from "vitest";
import { snesStateBlockDigests } from "./snes-state-diagnostics";

function block(tag: string, bytes: number[]) {
  const header = new TextEncoder().encode(`${tag}:${String(bytes.length).padStart(6, "0")}:`);
  return [...header, ...bytes];
}

describe("SNES state diagnostics", () => {
  it("returns bounded hashes without retaining state payloads", async () => {
    const state = new Uint8Array([
      ...new TextEncoder().encode("#!s9xsnp:0012\n"),
      ...block("CPU", [1, 2, 3]),
      ...block("RAM", [4, 5]),
    ]);

    const result = await snesStateBlockDigests(state);

    expect(result).toEqual([
      { tag: "CPU", start: 25, end: 28, digest: expect.stringMatching(/^[0-9a-f]{64}$/) },
      { tag: "RAM", start: 39, end: 41, digest: expect.stringMatching(/^[0-9a-f]{64}$/) },
    ]);
    expect(JSON.stringify(result)).not.toContain("1,2,3");
  });

  it.each([
    new Uint8Array(),
    new TextEncoder().encode("#!s9xsnp:0012\nCPU:000003:\u0001"),
    new TextEncoder().encode("#!s9xsnp:0012\nCPU:00x003:abc"),
  ])("fails closed for malformed state", async (state) => {
    expect(await snesStateBlockDigests(state)).toBeNull();
  });
});
