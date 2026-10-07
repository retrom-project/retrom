import { StrictMode } from "react";
import { cleanup, render } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { listenHorizontalWheel, useHorizontalWheel } from "./use-horizontal-wheel";

afterEach(cleanup);
function rail(width = 1000, viewport = 200) {
  const node = document.createElement("div");
  Object.defineProperties(node, {
    scrollWidth: { value: width, configurable: true },
    clientWidth: { value: viewport, configurable: true },
    scrollHeight: { value: 100, configurable: true },
    clientHeight: { value: 100, configurable: true },
  });
  return node;
}
function wheel(node: HTMLElement, init: WheelEventInit = {}) {
  const event = new WheelEvent("wheel", { bubbles: true, cancelable: true, deltaY: 40, ...init });
  node.dispatchEvent(event);
  return event;
}
it("moves horizontal content and consumes only the wheel direction that can move", () => {
  const node = rail();
  const stop = listenHorizontalWheel(node);
  expect(wheel(node).defaultPrevented).toBe(true);
  expect(node.scrollLeft).toBe(40);
  node.scrollLeft = 780;
  expect(wheel(node).defaultPrevented).toBe(true);
  expect(node.scrollLeft).toBe(800);
  expect(wheel(node).defaultPrevented).toBe(false);
  expect(wheel(node, { deltaY: -40 }).defaultPrevented).toBe(true);
  node.scrollLeft = 0;
  expect(wheel(node, { deltaY: -40 }).defaultPrevented).toBe(false);
  stop();
  expect(wheel(node).defaultPrevented).toBe(false);
});
it("leaves empty overflow, native horizontal gestures and zoom untouched", () => {
  for (const init of [{ deltaX: 10 }, { ctrlKey: true }, { metaKey: true }, { shiftKey: true }, { deltaY: 0 }]) {
    const node = rail();
    const stop = listenHorizontalWheel(node);
    expect(wheel(node, init).defaultPrevented).toBe(false);
    expect(node.scrollLeft).toBe(0);
    stop();
  }
  const node = rail(200);
  const stop = listenHorizontalWheel(node);
  expect(wheel(node).defaultPrevented).toBe(false);
  stop();
});
it("normalizes line and page wheel units", () => {
  const node = rail();
  const stop = listenHorizontalWheel(node);
  wheel(node, { deltaY: 2, deltaMode: WheelEvent.DOM_DELTA_LINE });
  expect(node.scrollLeft).toBe(32);
  wheel(node, { deltaY: 1, deltaMode: WheelEvent.DOM_DELTA_PAGE });
  expect(node.scrollLeft).toBe(232);
  stop();
});
it("forwards a custom scrollbar's horizontal gesture while preserving native gestures inside its scroller", () => {
  const area = document.createElement("div"), scroller = rail(), scrollbar = document.createElement("div");
  area.append(scroller, scrollbar);
  const stop = listenHorizontalWheel(area, scroller);
  expect(wheel(scrollbar, { deltaX: 30, deltaY: 0 }).defaultPrevented).toBe(true);
  expect(scroller.scrollLeft).toBe(30);
  expect(wheel(scroller, { deltaX: 30, deltaY: 0 }).defaultPrevented).toBe(false);
  expect(scroller.scrollLeft).toBe(30);
  stop();
});
it("preserves internal vertical scrolling including its boundary", () => {
  const outer = rail(), inner = rail();
  Object.defineProperty(inner, "scrollHeight", { value: 500 });
  inner.style.overflowY = "auto";
  const target = document.createElement("span");
  inner.append(target); outer.append(inner);
  const stop = listenHorizontalWheel(outer);
  expect(wheel(target).defaultPrevented).toBe(false);
  inner.scrollTop = 400;
  expect(wheel(target).defaultPrevented).toBe(false);
  expect(outer.scrollLeft).toBe(0);
  stop();
});
it("does not let an outer rail consume movement already handled by an inner rail", () => {
  const outer = rail(), inner = rail(500, 100);
  outer.append(inner);
  const stopOuter = listenHorizontalWheel(outer), stopInner = listenHorizontalWheel(inner);
  expect(wheel(inner).defaultPrevented).toBe(true);
  expect(inner.scrollLeft).toBe(40);
  expect(outer.scrollLeft).toBe(0);
  stopInner(); stopOuter();
});
it("attaches after conditional content appears and releases listeners on unmount", () => {
  function Probe({ visible }: { visible: boolean }) {
    const ref = useHorizontalWheel<HTMLDivElement>();
    return visible ? <div data-testid="rail" ref={ref} /> : null;
  }
  const view = render(<StrictMode><Probe visible={false} /></StrictMode>);
  view.rerender(<StrictMode><Probe visible /></StrictMode>);
  const node = view.getByTestId("rail");
  Object.defineProperties(node, { scrollWidth: { value: 500 }, clientWidth: { value: 100 } });
  expect(wheel(node).defaultPrevented).toBe(true);
  expect(node.scrollLeft).toBe(40);
  view.unmount();
  expect(wheel(node).defaultPrevented).toBe(false);
});
