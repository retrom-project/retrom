import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { cleanup, fireEvent, render } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { platformArt, platformArtwork } from "./platform-art";
import { HomePlatformArt } from "./home-platform-art";

afterEach(cleanup);
it("serves real local SVG files for every mapped platform, including family illustrations", () => {
  expect(platformArt("atom")).toBe("/images/platforms/bbc.svg");
  expect(platformArt("fds")).toBe("/images/platforms/nes.svg");
  let totalBytes = 0;
  for (const asset of new Set(Object.values(platformArtwork))) {
    const source = readFileSync(resolve("public/images/platforms", `${asset}.svg`));
    expect(source.toString()).toContain("<svg");
    expect(source.byteLength).toBeLessThanOrEqual(4096);
    totalBytes += source.byteLength;
  }
  expect(totalBytes).toBeLessThanOrEqual(128 * 1024);
});

it("never requests a guessed asset for an unknown platform", () => {
  expect(platformArt("unrecognized-platform")).toBeNull();
  const view = render(<HomePlatformArt platformId="unrecognized-platform" />);
  expect(view.container.querySelector("img")).toBeNull();
  expect(view.container.querySelector("svg")).toBeInTheDocument();
  expect(view.container.firstElementChild).toHaveClass("home-platform-art");
});

it("keeps the same artwork slot if a known local image fails to load", () => {
  const view = render(<HomePlatformArt platformId="atom" />);
  const image = view.container.querySelector("img")!;
  expect(new URL(image.src).pathname).toBe("/images/platforms/bbc.svg");
  fireEvent.error(image);
  expect(view.container.querySelector("img")).toBeNull();
  expect(view.container.querySelector("svg")).toBeInTheDocument();
  expect(view.container.firstElementChild).toHaveClass("home-platform-art");
});
