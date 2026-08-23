import { describe, expect, it } from "vitest";
import { containSize, fitCanvasToViewport } from "./canvas-fit";

describe("player canvas contain sizing", () => {
  it("expands a 4:3 game until its vertical edge reaches a 16:9 viewport", () => {
    expect(containSize(1920, 1080, 320, 240)).toEqual({ width: 1440, height: 1080 });
  });

  it("expands a widescreen game until its horizontal edge reaches a narrower viewport", () => {
    expect(containSize(1200, 1000, 1920, 1080)).toEqual({ width: 1200, height: 675 });
  });

  it("preserves the drawing-buffer ratio in CSS and rejects unavailable dimensions", () => {
    const canvas = document.createElement("canvas");
    canvas.width = 256;
    canvas.height = 224;
    expect(fitCanvasToViewport(canvas, 1600, 900)).toBe(true);
    expect(canvas.style.width).toBe(`${Math.round(900 * 256 / 224)}px`);
    expect(canvas.style.height).toBe("900px");
    expect(canvas.style.justifySelf).toBe("center");
    expect(canvas.style.alignSelf).toBe("center");
    expect(canvas.style.left).toBe(`${Math.floor((1600 - Math.round(900 * 256 / 224)) / 2)}px`);
    expect(canvas.style.top).toBe("0px");
    expect(containSize(0, 900, 256, 224)).toBeNull();
  });

  it("uses the core-reported portrait aspect instead of a landscape drawing buffer", () => {
    const canvas = document.createElement("canvas");
    canvas.width = 1920;
    canvas.height = 1080;
    expect(fitCanvasToViewport(canvas, 1920, 1080, 3 / 4)).toBe(true);
    expect(canvas.style.width).toBe("810px");
    expect(canvas.style.height).toBe("1080px");
  });

  it("uses an integer CSS box and origin for fractional contained geometry", () => {
    const canvas = document.createElement("canvas");
    canvas.width = 256;
    canvas.height = 192;
    expect(fitCanvasToViewport(canvas, 1280, 800, 256 / 196)).toBe(true);
    expect(canvas.style.width).toBe("1045px");
    expect(canvas.style.height).toBe("800px");
    expect(canvas.style.left).toBe("117px");
    expect(canvas.style.top).toBe("0px");
  });
});
