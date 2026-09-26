import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { platformArt } from "./platform-art";

const webRoot = path.resolve(import.meta.dirname, "../..");
const artworkRoot = path.join(webRoot, "public/images/platforms");
const catalog = JSON.parse(readFileSync(path.join(webRoot, "../data/runtime-target-bindings/v1/catalog.json"), "utf8")) as {
  definitions: { platforms: { id: string }[] };
};

describe("local platform artwork", () => {
  it("covers every declared platform with an existing local SVG", () => {
    for (const { id } of catalog.definitions.platforms) {
      const source = platformArt(id);
      expect(source, id).toMatch(/^\/images\/platforms\/[a-z0-9-]+\.svg$/);
      expect(statSync(path.join(webRoot, "public", source!)).isFile(), id).toBe(true);
    }
  });

  it("keeps the complete collection small and independent of remote or embedded resources", () => {
    const files = readdirSync(artworkRoot).filter((file) => file.endsWith(".svg"));
    let bytes = 0;
    for (const file of files) {
      const buffer = readFileSync(path.join(artworkRoot, file));
      bytes += buffer.byteLength;
      expect(buffer.byteLength, file).toBeLessThanOrEqual(4 * 1024);
      const document = new DOMParser().parseFromString(buffer.toString(), "image/svg+xml");
      expect(document.querySelector("parsererror"), file).toBeNull();
      expect(document.documentElement.getAttribute("viewBox"), file).toMatch(/^0 0 \d+ \d+$/);
      expect(document.querySelector("script, image, foreignObject, text, style"), file).toBeNull();
      expect(buffer.toString(), file).not.toMatch(/(?:href\s*=|data:|@import|onload\s*=|onerror\s*=)/i);
    }
    expect(bytes).toBeLessThanOrEqual(128 * 1024);
  });

  it("does not turn unknown IDs or object properties into asset URLs", () => {
    for (const id of ["unknown", "constructor", "__proto__", "../../secret", "https://example.com/image.svg"]) {
      expect(platformArt(id)).toBeNull();
    }
  });
});
