import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { listenEdgeFade } from "./use-edge-fade-rail";

let notify: (() => void) | undefined;
beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class {
    constructor(callback: () => void) { notify = callback; }
    observe() {}
    disconnect() {}
  });
});
afterEach(() => { vi.unstubAllGlobals(); notify = undefined; });

function rail(width: number, viewport = 200) {
  const node = document.createElement("div");
  Object.defineProperties(node, {
    scrollWidth: { value: width, configurable: true },
    clientWidth: { value: viewport, configurable: true },
  });
  return node;
}

it("fades only the edges that still hide tags", () => {
  const node = rail(500);
  const stop = listenEdgeFade(node);
  expect(node.dataset).toMatchObject({ fadeStart: "false", fadeEnd: "true" });
  node.scrollLeft = 120;
  node.dispatchEvent(new Event("scroll"));
  expect(node.dataset).toMatchObject({ fadeStart: "true", fadeEnd: "true" });
  node.scrollLeft = 300;
  node.dispatchEvent(new Event("scroll"));
  expect(node.dataset).toMatchObject({ fadeStart: "true", fadeEnd: "false" });
  stop();
});

it("removes both fades when every tag fits after a resize", () => {
  const node = rail(500);
  const stop = listenEdgeFade(node);
  Object.defineProperty(node, "scrollWidth", { value: 180, configurable: true });
  notify?.();
  expect(node.dataset).toMatchObject({ fadeStart: "false", fadeEnd: "false" });
  stop();
});
