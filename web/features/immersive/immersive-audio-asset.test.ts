import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

describe("immersive background music asset", () => {
  it("locks the reviewed Insert Coin OGG bytes", () => {
    const audio = readFileSync(resolve(process.cwd(), "public/audio/immersive/insert-coin.ogg"));
    expect(createHash("sha256").update(audio).digest("hex")).toBe(
      "d53775cb2efd123d9dcf44572b9a25cb3ec8cdb3fa179f73b40b2f681005d431",
    );
  });
});
