import { afterEach, describe, expect, it } from "vitest";
import { installPlayerFrameStyle } from "./player-frame-style";

afterEach(() => { document.body.replaceChildren(); });

describe("installPlayerFrameStyle", () => {
  it("removes the redundant touch menu and lowers both side control zones", () => {
    const frame = document.createElement("iframe");
    document.body.append(frame);
    const frameDocument = frame.contentDocument!;
    const upstreamStyle = frameDocument.createElement("style");
    upstreamStyle.textContent = `
      .ejs_virtualGamepad_open { display: block; }
      .ejs_virtualGamepad_left, .ejs_virtualGamepad_right { position: absolute; bottom: 50px; }
    `;
    frameDocument.head.append(upstreamStyle);

    const menu = frameDocument.createElement("div");
    menu.className = "ejs_virtualGamepad_open";
    menu.style.display = "block";
    const left = frameDocument.createElement("div");
    left.className = "ejs_virtualGamepad_left";
    const right = frameDocument.createElement("div");
    right.className = "ejs_virtualGamepad_right";
    frameDocument.body.append(menu, left, right);

    const installed = installPlayerFrameStyle(frameDocument);
    const frameWindow = frame.contentWindow!;
    expect(installed.dataset.retromPlayerFrame).toBe("true");
    expect(frameWindow.getComputedStyle(menu).display).toBe("none");
    expect(frameWindow.getComputedStyle(left).bottom).toBe("20px");
    expect(frameWindow.getComputedStyle(right).bottom).toBe("20px");
  });
});
